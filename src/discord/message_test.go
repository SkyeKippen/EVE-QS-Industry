package discord

import (
	"QS-Indy/src/notify"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNotificationEmbed(t *testing.T) {
	n := notify.Notification{
		Title:  strings.Repeat("é", 300),
		Fields: []notify.Field{{Name: "Contract to", Value: ""}, {Name: "Item", Value: "Tritanium", Inline: true}},
	}
	embed := NotificationEmbed(n)

	if got := utf8.RuneCountInString(embed.Title); got != maxTitle {
		t.Errorf("title is %d characters, want %d", got, maxTitle)
	}
	if !strings.HasSuffix(embed.Title, "…") {
		t.Errorf("trimmed title should end with an ellipsis")
	}
	if embed.Fields[0].Value != "-" {
		t.Errorf("empty field value = %q, want a dash", embed.Fields[0].Value)
	}
	if !embed.Fields[1].Inline || embed.Fields[1].Value != "Tritanium" {
		t.Errorf("field = %+v", embed.Fields[1])
	}
	if embed.Thumbnail != nil || embed.Footer != nil || embed.Timestamp != nil {
		t.Errorf("unset thumbnail, footer and timestamp should be left out")
	}
}

func TestNotificationEmbedCapsFields(t *testing.T) {
	n := notify.Notification{Fields: make([]notify.Field, 30)}
	if got := len(NotificationEmbed(n).Fields); got != maxFields {
		t.Errorf("%d fields, want %d", got, maxFields)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	for _, name := range envNames {
		t.Setenv(name, "")
	}
	cfg, err := LoadConfigFromEnv()
	if cfg != nil || err != nil {
		t.Fatalf("with nothing set got %v, %v; want Discord off", cfg, err)
	}

	t.Setenv("DISCORD_BOT_TOKEN", "token")
	if _, err := LoadConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "DISCORD_GUILD_ID") {
		t.Fatalf("with only the token set got %v; want an error naming what is missing", err)
	}

	for _, name := range envNames {
		t.Setenv(name, name)
	}
	cfg, err = LoadConfigFromEnv()
	if err != nil || cfg.GuildID != "DISCORD_GUILD_ID" || cfg.RedirectURI != "DISCORD_CALLBACK_URL" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
	if u := cfg.AuthorizeURL("abc"); !strings.Contains(u, "scope=identify+guilds.join") || !strings.Contains(u, "state=abc") {
		t.Errorf("authorize URL %q is missing the scopes or state", u)
	}
}
