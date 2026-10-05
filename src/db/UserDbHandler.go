package db

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNotYourCharacter means the character isn't linked to the signed-in user.
var ErrNotYourCharacter = errors.New("that character is not linked to your account")

// UserCharacter is a character linked to a user.
type UserCharacter struct {
	CharacterID   int64
	CharacterName string
	AddedAt       time.Time
}

// LinkCharacter links a character to a user after they sign in with it and
// returns that user's ID. A userID of 0 is a plain sign-in: the character
// keeps the user it already has, or gets a new user of its own. Otherwise the
// character is added to userID as an alt, moving it from any user it was
// linked to before; a user left with no characters is deleted.
func LinkCharacter(ctx context.Context, characterID int64, characterName string, userID int64) (int64, error) {
	conn, err := connectDB()
	if err != nil {
		return 0, err
	}
	defer conn.Close(context.Background())

	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())

	var currentUserID int64
	err = tx.QueryRow(ctx,
		`SELECT user_id FROM meadow_works.user_characters WHERE character_id = $1 FOR UPDATE`,
		characterID).Scan(&currentUserID)
	linked := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	switch {
	case userID == 0 && linked:
		_, err = tx.Exec(ctx,
			`UPDATE meadow_works.user_characters SET character_name = $2 WHERE character_id = $1`,
			characterID, characterName)
		userID = currentUserID
	case userID == 0:
		if err := tx.QueryRow(ctx, `INSERT INTO meadow_works.users DEFAULT VALUES RETURNING user_id`).Scan(&userID); err != nil {
			return 0, err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO meadow_works.user_characters (character_id, user_id, character_name) VALUES ($1, $2, $3)`,
			characterID, userID, characterName)
	case linked && currentUserID == userID:
		_, err = tx.Exec(ctx,
			`UPDATE meadow_works.user_characters SET character_name = $2 WHERE character_id = $1`,
			characterID, characterName)
	default:
		_, err = tx.Exec(ctx,
			`INSERT INTO meadow_works.user_characters (character_id, user_id, character_name) VALUES ($1, $2, $3)
			ON CONFLICT (character_id) DO UPDATE SET user_id = $2, character_name = $3, added_at = now()`,
			characterID, userID, characterName)
		if err == nil && linked {
			err = deleteUserIfEmpty(ctx, tx, currentUserID)
		}
	}
	if err != nil {
		return 0, err
	}

	return userID, tx.Commit(ctx)
}

// ListUserCharacters returns the user's characters for the My Characters
// page: the one they first signed in with, then the rest by name.
func ListUserCharacters(ctx context.Context, userID int64) ([]UserCharacter, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx,
		`SELECT character_id, character_name, added_at FROM meadow_works.user_characters WHERE user_id = $1`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	characters := make([]UserCharacter, 0)
	for rows.Next() {
		var character UserCharacter
		if err := rows.Scan(&character.CharacterID, &character.CharacterName, &character.AddedAt); err != nil {
			return nil, err
		}
		characters = append(characters, character)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sortMainFirst(characters)
	return characters, nil
}

// sortMainFirst puts the earliest added character first and the others in
// name order.
func sortMainFirst(characters []UserCharacter) {
	if len(characters) == 0 {
		return
	}
	main := 0
	for i, character := range characters {
		if character.AddedAt.Before(characters[main].AddedAt) {
			main = i
		}
	}
	characters[0], characters[main] = characters[main], characters[0]

	alts := characters[1:]
	sort.Slice(alts, func(i, j int) bool {
		return alts[i].CharacterName < alts[j].CharacterName
	})
}

// DeleteCharacterData removes everything the app holds for a character once
// it is deauthorized: its saved token, the blueprints it owns, and its link
// to its user (deleting the user when it was their last character). The
// blueprints of corporationID go too, unless it is 0; the caller decides
// whether another character still needs them. It returns the refresh token
// so it can be revoked with EVE SSO, or "" when there was none or it couldn't
// be decrypted.
func DeleteCharacterData(ctx context.Context, characterID, corporationID int64) (string, error) {
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

	var encRefresh []byte
	err = tx.QueryRow(ctx,
		`DELETE FROM meadow_works.esi_tokens WHERE character_id = $1 RETURNING refresh_token`,
		characterID).Scan(&encRefresh)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	_, err = tx.Exec(ctx,
		`DELETE FROM meadow_works.blueprints WHERE owner_id = $1 AND owner_type = $2`,
		characterID, OwnerCharacter)
	if err != nil {
		return "", err
	}
	if corporationID != 0 {
		_, err = tx.Exec(ctx,
			`DELETE FROM meadow_works.blueprints WHERE owner_id = $1 AND owner_type = $2`,
			corporationID, OwnerCorporation)
		if err != nil {
			return "", err
		}
	}

	var userID int64
	err = tx.QueryRow(ctx,
		`DELETE FROM meadow_works.user_characters WHERE character_id = $1 RETURNING user_id`,
		characterID).Scan(&userID)
	if err == nil {
		err = deleteUserIfEmpty(ctx, tx, userID)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	if encRefresh == nil {
		return "", nil
	}
	refreshToken, err := decryptToken(encRefresh)
	if err != nil {
		// the row is gone either way, which is what stops the app using it
		return "", nil
	}
	return refreshToken, nil
}

func deleteUserIfEmpty(ctx context.Context, tx pgx.Tx, userID int64) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM meadow_works.users u WHERE user_id = $1
		AND NOT EXISTS (SELECT 1 FROM meadow_works.user_characters c WHERE c.user_id = u.user_id)`,
		userID)
	return err
}
