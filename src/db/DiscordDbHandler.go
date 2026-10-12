package db

import (
	"QS-Indy/src/notify"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// DiscordLink is the Discord account a user linked.
type DiscordLink struct {
	DiscordUserID string
	DiscordName   string
	LinkedAt      time.Time
}

// LoadDiscordLink returns the user's Discord link, or ok false when they
// have none.
func LoadDiscordLink(ctx context.Context, userID int64) (link DiscordLink, ok bool, err error) {
	conn, err := connectDB()
	if err != nil {
		return DiscordLink{}, false, err
	}
	defer conn.Close(context.Background())

	err = conn.QueryRow(ctx,
		`SELECT discord_user_id, discord_name, linked_at FROM meadow_works.discord_links WHERE user_id = $1`,
		userID).Scan(&link.DiscordUserID, &link.DiscordName, &link.LinkedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DiscordLink{}, false, nil
	}
	if err != nil {
		return DiscordLink{}, false, err
	}
	return link, true, nil
}

// SaveDiscordLink links a Discord account to the user, replacing any account
// they linked before. A Discord account links to one user at a time, so it
// is taken from any other user it was linked to.
func SaveDiscordLink(ctx context.Context, userID int64, discordUserID, discordName string) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(ctx,
		`DELETE FROM meadow_works.discord_links WHERE discord_user_id = $1 AND user_id <> $2`,
		discordUserID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO meadow_works.discord_links (user_id, discord_user_id, discord_name) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET discord_user_id = $2, discord_name = $3, linked_at = now()`,
		userID, discordUserID, discordName); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteDiscordLink unlinks the user's Discord account. Their notification
// settings are kept for when they link one again.
func DeleteDiscordLink(ctx context.Context, userID int64) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	_, err = conn.Exec(ctx, `DELETE FROM meadow_works.discord_links WHERE user_id = $1`, userID)
	return err
}

// LoadNotificationSettings returns whether each event is on for the user.
// Events they never changed are on.
func LoadNotificationSettings(ctx context.Context, userID int64) (map[notify.Event]bool, error) {
	conn, err := connectDB()
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(ctx,
		`SELECT event, enabled FROM meadow_works.notification_settings WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := map[notify.Event]bool{}
	for _, info := range notify.Events() {
		settings[info.Event] = true
	}
	for rows.Next() {
		var event string
		var enabled bool
		if err := rows.Scan(&event, &enabled); err != nil {
			return nil, err
		}
		settings[notify.Event(event)] = enabled
	}
	return settings, rows.Err()
}

// SaveNotificationSettings stores whether each event in settings is on for
// the user.
func SaveNotificationSettings(ctx context.Context, userID int64, settings map[notify.Event]bool) error {
	conn, err := connectDB()
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	batch := &pgx.Batch{}
	for event, enabled := range settings {
		batch.Queue(`INSERT INTO meadow_works.notification_settings (user_id, event, enabled) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, event) DO UPDATE SET enabled = $3`,
			userID, string(event), enabled)
	}
	return conn.SendBatch(ctx, batch).Close()
}

// NotificationRecipient is a notify.RecipientLookup: it returns the user's
// linked Discord account, unless they have none or turned the event off.
func NotificationRecipient(ctx context.Context, userID int64, event notify.Event) (notify.Recipient, bool, error) {
	conn, err := connectDB()
	if err != nil {
		return notify.Recipient{}, false, err
	}
	defer conn.Close(context.Background())

	var discordUserID string
	err = conn.QueryRow(ctx,
		`SELECT l.discord_user_id FROM meadow_works.discord_links l
		WHERE l.user_id = $1
		AND COALESCE((SELECT s.enabled FROM meadow_works.notification_settings s
			WHERE s.user_id = l.user_id AND s.event = $2), true)`,
		userID, string(event)).Scan(&discordUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return notify.Recipient{}, false, nil
	}
	if err != nil {
		return notify.Recipient{}, false, err
	}
	return notify.Recipient{DiscordUserID: discordUserID}, true, nil
}

// UserIDForCharacterName returns the user the named character is linked to,
// or ok false when no user has it. Orders record their owner by character
// name, so this finds who to tell about one.
func UserIDForCharacterName(ctx context.Context, characterName string) (userID int64, ok bool, err error) {
	conn, err := connectDB()
	if err != nil {
		return 0, false, err
	}
	defer conn.Close(context.Background())

	err = conn.QueryRow(ctx,
		`SELECT user_id FROM meadow_works.user_characters WHERE character_name = $1
		ORDER BY added_at LIMIT 1`, characterName).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return userID, true, nil
}
