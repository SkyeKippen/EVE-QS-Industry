package auth

import (
	"QS-Indy/src/esi"
	"context"
	"errors"
	"log"
	"net/http"
	"time"
)

var cfg *esi.Config

const (
	stateCookieName    = "eve_sso_state"
	verifierCookieName = "eve_sso_verifier"
	addAltCookieName   = "eve_sso_add_alt"
)

// addAltParam on /auth/login marks a sign-in that adds an alt to the
// signed-in user instead of starting a new session.
const addAltParam = "add_alt"

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := esi.GenerateState()
	if err != nil {
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		log.Printf("evesso: state generation failed: %v", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(10 * time.Minute),
	})

	var pkce *esi.PKCE
	if cfg.UsePKCE {
		p, err := esi.GeneratePKCE()
		if err != nil {
			http.Error(w, "failed to start login", http.StatusInternalServerError)
			log.Printf("evesso: pkce generation failed: %v", err)
			return
		}
		pkce = &p

		http.SetCookie(w, &http.Cookie{
			Name:     verifierCookieName,
			Value:    pkce.Verifier,
			Path:     "/auth",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(10 * time.Minute),
		})
	}

	if r.URL.Query().Has(addAltParam) && IsLoggedIn(r) {
		http.SetCookie(w, &http.Cookie{
			Name:     addAltCookieName,
			Value:    "1",
			Path:     "/auth",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(10 * time.Minute),
		})
	} else {
		http.SetCookie(w, &http.Cookie{Name: addAltCookieName, Path: "/auth", MaxAge: -1})
	}

	http.Redirect(w, r, cfg.BuildAuthorizeURL(state, pkce, requestedScopes(r)), http.StatusFound)
}

func HandleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	returnedState := q.Get("state")

	if code == "" || returnedState == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value != returnedState {
		http.Error(w, "invalid state - possible CSRF", http.StatusBadRequest)
		return
	}

	var verifier string
	if cfg.UsePKCE {
		verifierCookie, err := r.Cookie(verifierCookieName)
		if err != nil {
			http.Error(w, "missing pkce verifier", http.StatusBadRequest)
			return
		}
		verifier = verifierCookie.Value
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	tok, err := cfg.ExchangeCode(ctx, code, verifier)
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		log.Printf("evesso: exchange failed: %v", err)
		return
	}

	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Path: "/auth", MaxAge: -1})
	if cfg.UsePKCE {
		http.SetCookie(w, &http.Cookie{Name: verifierCookieName, Path: "/auth", MaxAge: -1})
	}

	characterID, characterName, err := decodeCharacterFromAccessToken(tok.AccessToken)
	if err != nil {
		http.Error(w, "failed to read character info", http.StatusBadGateway)
		log.Printf("evesso: decoding access token: %v", err)
		return
	}

	// adding an alt keeps the current session and links the new character to its user
	var addToUserID int64
	if c, err := r.Cookie(addAltCookieName); err == nil && c.Value == "1" {
		if sess, ok := CurrentSession(r); ok {
			addToUserID = sess.UserID
		}
	}
	http.SetCookie(w, &http.Cookie{Name: addAltCookieName, Path: "/auth", MaxAge: -1})

	userID, err := linkCharacter(ctx, characterID, characterName, addToUserID)
	if err != nil {
		http.Error(w, "failed to link character to your account", http.StatusInternalServerError)
		log.Printf("evesso: linking character %d (%s) to user %d: %v", characterID, characterName, addToUserID, err)
		return
	}

	redirect := "/"
	if addToUserID != 0 {
		log.Printf("handleCallback: added alt %d (%s) to user %d", characterID, characterName, userID)
		redirect = "/characters"
	} else {
		if err := SetSessionCookie(w, userID, characterID, characterName); err != nil {
			http.Error(w, "failed to start session", http.StatusInternalServerError)
			log.Printf("evesso: creating session: %v", err)
			return
		}
		log.Printf("handleCallback: session set for character %d (%s), user %d", characterID, characterName, userID)
	}

	// a failed save shouldn't block sign-in; ESI features will report the missing token
	scopes, err := decodeScopesFromAccessToken(tok.AccessToken)
	if err != nil {
		log.Printf("evesso: reading scopes for character %d: %v", characterID, err)
	}
	err = saveToken(ctx, esi.CharacterToken{
		CharacterID:   characterID,
		CharacterName: characterName,
		Scopes:        scopes,
		AccessToken:   tok.AccessToken,
		RefreshToken:  tok.RefreshToken,
		ExpiresAt:     tok.ExpiresAt,
	})
	if err != nil {
		log.Printf("evesso: saving token for character %d (%s): %v", characterID, characterName, err)
	}

	http.Redirect(w, r, redirect, http.StatusFound)
}

// CharacterLinker links a character to a user after they sign in with it and
// returns the user's ID; addToUserID is 0 for a plain sign-in, or the user an
// alt is being added to. It lives outside this package for the same reason as
// TokenSaver.
type CharacterLinker func(ctx context.Context, characterID int64, characterName string, addToUserID int64) (int64, error)

var linkCharacter CharacterLinker

// TokenSaver stores a character's tokens after they sign in. It lives outside
// this package because the db package already imports auth.
type TokenSaver func(ctx context.Context, token esi.CharacterToken) error

var saveToken TokenSaver

func InitAuth(tokenSaver TokenSaver, characterLinker CharacterLinker) error {
	if tokenSaver == nil {
		return errors.New("evesso: a TokenSaver is required")
	}
	if characterLinker == nil {
		return errors.New("evesso: a CharacterLinker is required")
	}
	saveToken = tokenSaver
	linkCharacter = characterLinker

	var err error
	cfg, err = esi.LoadConfigFromEnv()
	return err
}

// Config returns the SSO config loaded by InitAuth, for refreshing tokens.
func Config() *esi.Config {
	return cfg
}
