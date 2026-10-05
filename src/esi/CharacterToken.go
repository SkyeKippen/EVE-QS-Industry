package esi

import "time"

// CharacterToken is a signed-in character's SSO token pair, as saved by the
// web app's sign-in and loaded by anything that calls authenticated ESI routes.
type CharacterToken struct {
	CharacterID   int64
	CharacterName string
	Scopes        []string
	AccessToken   string
	RefreshToken  string
	ExpiresAt     time.Time
}
