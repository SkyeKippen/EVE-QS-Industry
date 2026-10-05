package main

import (
	"QS-Indy/src/auth"
	"QS-Indy/src/db"
	"QS-Indy/src/esi"
	"context"
	"log"
	"net/http"
	"slices"
	"strconv"
)

func renderCharacters(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.CurrentSession(r)

	characters, err := db.ListUserCharacters(r.Context(), sess.UserID)
	if err != nil {
		log.Printf("renderCharacters: user %d: %v", sess.UserID, err)
		http.Error(w, "could not load your characters", http.StatusInternalServerError)
		return
	}

	// CharacterID marks the signed-in character's row.
	data := struct {
		CharacterID int64
		Characters  []db.UserCharacter
	}{CharacterID: sess.CharacterID, Characters: characters}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "characters.html", data); err != nil {
		log.Printf("renderCharacters: %v", err)
	}
}

func renderAddAltCharacter(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.CurrentSession(r)

	data := struct {
		CharacterName string
		AddAlt        bool
		ScopeChoices  []auth.ScopeChoice
	}{CharacterName: sess.CharacterName, AddAlt: true, ScopeChoices: auth.ScopeChoices()}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "characters_add.html", data); err != nil {
		log.Printf("renderAddAltCharacter: %v", err)
	}
}

// handleDeauthorizeCharacter deauthorizes one of the signed-in user's
// characters and clears its data. Deauthorizing the signed-in character
// switches the session to the user's next character, or signs out when it
// was the last one.
func handleDeauthorizeCharacter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, _ := auth.CurrentSession(r)

	characterID, err := strconv.ParseInt(r.PostFormValue("character-id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid character id", http.StatusBadRequest)
		return
	}

	characters, err := db.ListUserCharacters(r.Context(), sess.UserID)
	if err != nil {
		log.Printf("deauthorize: listing characters of user %d: %v", sess.UserID, err)
		http.Error(w, "failed to deauthorize character", http.StatusInternalServerError)
		return
	}
	i := slices.IndexFunc(characters, func(c db.UserCharacter) bool { return c.CharacterID == characterID })
	if i < 0 {
		http.Error(w, db.ErrNotYourCharacter.Error(), http.StatusForbidden)
		return
	}

	if err := deauthorizeCharacters(r.Context(), characters[i:i+1]); err != nil {
		http.Error(w, "failed to deauthorize character", http.StatusInternalServerError)
		return
	}

	if characterID == sess.CharacterID {
		remaining := slices.Delete(characters, i, i+1)
		if len(remaining) == 0 {
			auth.ClearSessionCookie(w, r)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		auth.SwitchSessionCharacter(r, remaining[0].CharacterID, remaining[0].CharacterName)
	}
	http.Redirect(w, r, "/characters", http.StatusSeeOther)
}

// handleDeauthorizeAll deauthorizes every character of the signed-in user,
// clears their data, and signs out.
func handleDeauthorizeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, _ := auth.CurrentSession(r)

	characters, err := db.ListUserCharacters(r.Context(), sess.UserID)
	if err != nil {
		log.Printf("deauthorize: listing characters of user %d: %v", sess.UserID, err)
		http.Error(w, "failed to deauthorize characters", http.StatusInternalServerError)
		return
	}
	// a session from before users existed may have no linked characters
	if !slices.ContainsFunc(characters, func(c db.UserCharacter) bool { return c.CharacterID == sess.CharacterID }) {
		characters = append(characters, db.UserCharacter{CharacterID: sess.CharacterID, CharacterName: sess.CharacterName})
	}

	if err := deauthorizeCharacters(r.Context(), characters); err != nil {
		http.Error(w, "failed to deauthorize characters", http.StatusInternalServerError)
		return
	}

	auth.ClearSessionCookie(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// deauthorizeCharacters deletes each character's token and data and revokes
// the token with EVE SSO. A character's corporation blueprints are deleted
// too when no character left in the app is in that corporation.
func deauthorizeCharacters(ctx context.Context, characters []db.UserCharacter) error {
	removing := make([]int64, len(characters))
	for i, character := range characters {
		removing[i] = character.CharacterID
	}
	corporations := corporationsToClear(ctx, removing)

	for _, character := range characters {
		refreshToken, err := db.DeleteCharacterData(ctx, character.CharacterID, corporations[character.CharacterID])
		if err != nil {
			log.Printf("deauthorize: clearing data of character %d (%s): %v", character.CharacterID, character.CharacterName, err)
			return err
		}
		// the token is already gone from the database, so a failed SSO revoke only gets logged
		if refreshToken != "" {
			if err := auth.Config().RevokeRefreshToken(ctx, refreshToken); err != nil {
				log.Printf("deauthorize: EVE SSO revoke for character %d: %v", character.CharacterID, err)
			}
		}
		log.Printf("deauthorize: cleared character %d (%s), corporation blueprints cleared: %d",
			character.CharacterID, character.CharacterName, corporations[character.CharacterID])
	}
	return nil
}

// corporationsToClear maps each character being removed to its corporation
// when no other character with a saved token, of any user, is in that
// corporation, so its blueprints can go. Corporations are kept whenever that
// can't be worked out.
func corporationsToClear(ctx context.Context, removing []int64) map[int64]int64 {
	toClear := map[int64]int64{}

	tokenCharacters, err := db.ListTokenCharacters(ctx)
	if err != nil {
		log.Printf("deauthorize: listing characters to check corporations, keeping corporation blueprints: %v", err)
		return toClear
	}
	covered := map[int64]bool{}
	for _, character := range tokenCharacters {
		if slices.Contains(removing, character.CharacterID) {
			continue
		}
		corporationID, err := esi.GetCharacterCorporationCached(int(character.CharacterID))
		if err != nil {
			log.Printf("deauthorize: corporation of character %d unknown, keeping corporation blueprints: %v", character.CharacterID, err)
			return map[int64]int64{}
		}
		covered[corporationID] = true
	}

	for _, characterID := range removing {
		corporationID, err := esi.GetCharacterCorporationCached(int(characterID))
		if err != nil {
			log.Printf("deauthorize: corporation of character %d unknown, keeping its blueprints: %v", characterID, err)
			continue
		}
		if !covered[corporationID] {
			toClear[characterID] = corporationID
		}
	}
	return toClear
}
