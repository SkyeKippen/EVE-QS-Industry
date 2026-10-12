package discord

import (
	"QS-Indy/src/notify"
	"context"
	"time"
	"unicode/utf8"
)

// Message is the body of a message the bot sends.
type Message struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

// Embed is a Discord rich embed.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	URL         string       `json:"url,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Thumbnail   *EmbedImage  `json:"thumbnail,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   *time.Time   `json:"timestamp,omitempty"`
}

type EmbedImage struct {
	URL string `json:"url"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type EmbedFooter struct {
	Text string `json:"text"`
}

// Discord rejects embeds over these lengths, counted in characters.
const (
	maxTitle       = 256
	maxDescription = 4096
	maxFieldName   = 256
	maxFieldValue  = 1024
	maxFields      = 25
	maxFooter      = 2048
)

// NotificationEmbed turns a notification into an embed, trimming anything
// longer than Discord allows. Empty field values become a dash because
// Discord rejects empty fields.
func NotificationEmbed(n notify.Notification) Embed {
	embed := Embed{
		Title:       truncate(n.Title, maxTitle),
		URL:         n.URL,
		Description: truncate(n.Description, maxDescription),
		Color:       n.Color,
	}
	if n.ThumbnailURL != "" {
		embed.Thumbnail = &EmbedImage{URL: n.ThumbnailURL}
	}
	for i, f := range n.Fields {
		if i == maxFields {
			break
		}
		value := f.Value
		if value == "" {
			value = "-"
		}
		embed.Fields = append(embed.Fields, EmbedField{
			Name:   truncate(f.Name, maxFieldName),
			Value:  truncate(value, maxFieldValue),
			Inline: f.Inline,
		})
	}
	if n.Footer != "" {
		embed.Footer = &EmbedFooter{Text: truncate(n.Footer, maxFooter)}
	}
	if !n.Timestamp.IsZero() {
		ts := n.Timestamp.UTC()
		embed.Timestamp = &ts
	}
	return embed
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

// Notifier delivers notifications as DMs from the bot.
type Notifier struct {
	Config *Config
}

func (d Notifier) Send(ctx context.Context, recipient notify.Recipient, n notify.Notification) error {
	if recipient.DiscordUserID == "" {
		return nil
	}
	return d.Config.SendDM(ctx, recipient.DiscordUserID, Message{Embeds: []Embed{NotificationEmbed(n)}})
}
