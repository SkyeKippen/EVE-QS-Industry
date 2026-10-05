package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type accessTokenClaims struct {
	Subject string          `json:"sub"`  // "CHARACTER:EVE:2118075319"
	Name    string          `json:"name"` // character display name
	Issuer  string          `json:"iss"`
	Scopes  json.RawMessage `json:"scp"` // a single string, or an array when there are several scopes
}

func decodeAccessTokenClaims(accessToken string) (accessTokenClaims, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return accessTokenClaims{}, fmt.Errorf("not a valid JWT: expected 3 parts, got %d", len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return accessTokenClaims{}, fmt.Errorf("decoding JWT payload: %w", err)
	}

	var claims accessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return accessTokenClaims{}, fmt.Errorf("parsing JWT claims: %w", err)
	}
	return claims, nil
}

func decodeCharacterFromAccessToken(accessToken string) (characterID int64, characterName string, err error) {
	claims, err := decodeAccessTokenClaims(accessToken)
	if err != nil {
		return 0, "", err
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

// decodeScopesFromAccessToken returns the scopes the character granted.
func decodeScopesFromAccessToken(accessToken string) ([]string, error) {
	claims, err := decodeAccessTokenClaims(accessToken)
	if err != nil {
		return nil, err
	}
	if len(claims.Scopes) == 0 {
		return nil, nil
	}

	var scopes []string
	if err := json.Unmarshal(claims.Scopes, &scopes); err == nil {
		return scopes, nil
	}
	var scope string
	if err := json.Unmarshal(claims.Scopes, &scope); err != nil {
		return nil, fmt.Errorf("parsing scp claim: %w", err)
	}
	return []string{scope}, nil
}
