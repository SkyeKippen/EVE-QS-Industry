package db

import (
	"QS-Indy/src/esi"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoToken means the character has never signed in to the web app, so
// there is no token to call ESI with.
var ErrNoToken = errors.New("no saved token for this character; they need to sign in to the web app")

// refresh a little early so a token doesn't expire mid-request
const tokenExpiryMargin = time.Minute

// TokenCharacter is a character with a saved token.
type TokenCharacter struct {
	CharacterID   int64
	CharacterName string
	Scopes        []string
}

// SaveCharacterToken stores or replaces a character's tokens. It matches
// auth.TokenSaver and is called after every sign-in.
func SaveCharacterToken(ctx context.Context, token esi.CharacterToken) error {
	accessToken, err := encryptToken(token.AccessToken)
	if err != nil {
		return err
	}
	refreshToken, err := encryptToken(token.RefreshToken)
	if err != nil {
		return err
	}

	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	_, err = conn.Exec(ctx,
		`INSERT INTO meadow_works.esi_tokens
		(character_id, character_name, scopes, access_token, refresh_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (character_id) DO UPDATE
		SET character_name = $2, scopes = $3, access_token = $4, refresh_token = $5, expires_at = $6, updated_at = now()`,
		token.CharacterID, token.CharacterName, strings.Join(token.Scopes, " "), accessToken, refreshToken, token.ExpiresAt)
	return err
}

// GetAccessToken returns a usable access token for the character, refreshing
// it through EVE SSO and saving the new pair when the stored one has expired.
// The row is locked while refreshing, since EVE may rotate the refresh token
// and two refreshes racing would leave one of them holding a dead token.
func GetAccessToken(ctx context.Context, cfg *esi.Config, characterID int64) (string, error) {
	conn, err := connectDB()
	if err != nil {
		return "", err
	}
	defer conn.Close(context.Background())

	tx, err := conn.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())

	var encAccess, encRefresh []byte
	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT access_token, refresh_token, expires_at FROM meadow_works.esi_tokens
		WHERE character_id = $1 FOR UPDATE`, characterID).Scan(&encAccess, &encRefresh, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoToken
	}
	if err != nil {
		return "", err
	}

	if time.Now().Add(tokenExpiryMargin).Before(expiresAt) {
		return decryptToken(encAccess)
	}

	refreshToken, err := decryptToken(encRefresh)
	if err != nil {
		return "", err
	}

	tok, err := cfg.RefreshAccessToken(ctx, refreshToken)
	if err != nil {
		return "", fmt.Errorf("refreshing token for character %d (they may need to sign in again): %w", characterID, err)
	}
	if tok.RefreshToken != "" {
		refreshToken = tok.RefreshToken
	}

	newAccess, err := encryptToken(tok.AccessToken)
	if err != nil {
		return "", err
	}
	newRefresh, err := encryptToken(refreshToken)
	if err != nil {
		return "", err
	}

	_, err = tx.Exec(ctx,
		`UPDATE meadow_works.esi_tokens
		SET access_token = $2, refresh_token = $3, expires_at = $4, updated_at = now()
		WHERE character_id = $1`,
		characterID, newAccess, newRefresh, tok.ExpiresAt)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// DeleteCharacterToken removes a character's saved token so the app stops
// calling ESI for them, and returns the refresh token so it can be revoked
// with EVE SSO too. It returns ErrNoToken when nothing was saved, and an empty
// refresh token when the row was deleted but its token couldn't be decrypted.
func DeleteCharacterToken(ctx context.Context, characterID int64) (string, error) {
	conn, err := connectDB()
	if err != nil {
		return "", err
	}
	defer conn.Close(context.Background())

	var encRefresh []byte
	err = conn.QueryRow(ctx,
		`DELETE FROM meadow_works.esi_tokens WHERE character_id = $1 RETURNING refresh_token`,
		characterID).Scan(&encRefresh)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoToken
	}
	if err != nil {
		return "", err
	}
	refreshToken, err := decryptToken(encRefresh)
	if err != nil {
		// the row is gone either way, which is what stops the app using it
		return "", nil
	}
	return refreshToken, nil
}

// ListTokenCharacters returns every character with a saved token.
func ListTokenCharacters(ctx context.Context) ([]TokenCharacter, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx,
		`SELECT character_id, character_name, scopes FROM meadow_works.esi_tokens ORDER BY character_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	characters := make([]TokenCharacter, 0)
	for rows.Next() {
		var character TokenCharacter
		var scopes string
		if err := rows.Scan(&character.CharacterID, &character.CharacterName, &scopes); err != nil {
			return nil, err
		}
		character.Scopes = strings.Fields(scopes)
		characters = append(characters, character)
	}
	return characters, rows.Err()
}

// ResolveTokenCharacter picks a character with a saved token, for command
// line tools. A characterID of 0 means "the only one saved".
func ResolveTokenCharacter(ctx context.Context, characterID int64) (TokenCharacter, error) {
	characters, err := ListTokenCharacters(ctx)
	if err != nil {
		return TokenCharacter{}, err
	}
	if len(characters) == 0 {
		return TokenCharacter{}, errors.New("no saved tokens; sign in to the web app first")
	}

	if characterID == 0 {
		if len(characters) == 1 {
			return characters[0], nil
		}
		return TokenCharacter{}, fmt.Errorf("several characters have tokens, pick one with -character: %s", describeCharacters(characters))
	}

	for _, character := range characters {
		if character.CharacterID == characterID {
			return character, nil
		}
	}
	return TokenCharacter{}, fmt.Errorf("character %d has no saved token; saved: %s", characterID, describeCharacters(characters))
}

func describeCharacters(characters []TokenCharacter) string {
	names := make([]string, len(characters))
	for i, character := range characters {
		names[i] = fmt.Sprintf("%s (%d)", character.CharacterName, character.CharacterID)
	}
	return strings.Join(names, ", ")
}

// tokenCipher builds the AES-256-GCM cipher from TOKEN_ENCRYPTION_KEY, a
// base64-encoded 32 byte key.
func tokenCipher() (cipher.AEAD, error) {
	rawKey := os.Getenv("TOKEN_ENCRYPTION_KEY")
	if rawKey == "" {
		return nil, errors.New("TOKEN_ENCRYPTION_KEY is not set in " + esi.EnvFile)
	}
	key, err := base64.StdEncoding.DecodeString(rawKey)
	if err != nil {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY is not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("TOKEN_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encryptToken returns the nonce followed by the sealed token.
func encryptToken(token string) ([]byte, error) {
	gcm, err := tokenCipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(token), nil), nil
}

func decryptToken(sealed []byte) (string, error) {
	gcm, err := tokenCipher()
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("stored token is too short to decrypt")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("could not decrypt stored token (was TOKEN_ENCRYPTION_KEY changed?): %w", err)
	}
	return string(plain), nil
}
