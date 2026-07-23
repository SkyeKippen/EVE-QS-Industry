package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type accessTokenClaims struct {
	Subject string `json:"sub"`  // "CHARACTER:EVE:2118075319"
	Name    string `json:"name"` // character display name
	Issuer  string `json:"iss"`
}

func decodeCharacterFromAccessToken(accessToken string) (characterID int64, characterName string, err error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return 0, "", fmt.Errorf("not a valid JWT: expected 3 parts, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, "", fmt.Errorf("decoding JWT payload: %w", err)
	}

	var claims accessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0, "", fmt.Errorf("parsing JWT claims: %w", err)
	}

	const prefix = "CHARACTER:EVE:"
	if !strings.HasPrefix(claims.Subject, prefix) {
		return 0, "", fmt.Errorf("unexpected sub claim format: %q", claims.Subject)
	}
	idStr := strings.TrimPrefix(claims.Subject, prefix)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("parsing character id from sub claim: %w", err)
	}

	if claims.Name == "" {
		return 0, "", fmt.Errorf("token missing name claim")
	}

	return id, claims.Name, nil
}
