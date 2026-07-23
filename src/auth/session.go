package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"os"
	"sync"
	"time"
)

type Session struct {
	CharacterID   int64
	CharacterName string
	ExpiresAt     time.Time
}

const (
	sessionCookieName = "app_session"
	sessionTTL        = 24 * time.Hour
)

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session // sessionID -> session
}

var store = &sessionStore{sessions: make(map[string]*Session)}

func (s *sessionStore) create(characterID int64, characterName string) (id string, err error) {
	id, err = randomID(32)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[id] = &Session{
		CharacterID:   characterID,
		CharacterName: characterName,
		ExpiresAt:     time.Now().Add(sessionTTL),
	}
	s.mu.Unlock()
	return id, nil
}

func (s *sessionStore) get(id string) (*Session, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.ExpiresAt) {
		s.delete(id)
		return nil, false
	}
	return sess, true
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sessionSecret() []byte {
	if s := os.Getenv("SESSION_SECRET"); s != "" {
		return []byte(s)
	}
	return []byte("development-secret-perhaps")
}

func signSessionID(id string) string {
	mac := hmac.New(sha256.New, sessionSecret())
	mac.Write([]byte(id))
	sig := hex.EncodeToString(mac.Sum(nil))
	return id + "." + sig
}

func verifySessionCookie(value string) (id string, ok bool) {
	sep := -1
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] == '.' {
			sep = i
			break
		}
	}
	if sep < 0 {
		return "", false
	}
	id, sig := value[:sep], value[sep+1:]

	mac := hmac.New(sha256.New, sessionSecret())
	mac.Write([]byte(id))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", false
	}
	return id, true
}

func SetSessionCookie(w http.ResponseWriter, characterID int64, characterName string) error {
	id, err := store.create(characterID, characterName)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    signSessionID(id),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
	return nil
}

func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		if id, ok := verifySessionCookie(c.Value); ok {
			store.delete(id)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1})
}

func CurrentSession(r *http.Request) (*Session, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, false
	}
	id, ok := verifySessionCookie(c.Value)
	if !ok {
		return nil, false
	}
	return store.get(id)
}

func IsLoggedIn(r *http.Request) bool {
	_, ok := CurrentSession(r)
	return ok
}

func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !IsLoggedIn(r) {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}
