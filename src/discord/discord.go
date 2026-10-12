// Package discord links a user's Discord account through Discord sign-in,
// adds them to the app's Discord server, and sends them DMs from the bot.
// It only calls Discord's HTTP API; nothing stays connected to the gateway.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	apiBase      = "https://discord.com/api/v10"
	authorizeURL = "https://discord.com/oauth2/authorize"
	tokenURL     = apiBase + "/oauth2/token"

	// identify reads who signed in; guilds.join lets the bot add them to
	// the server, which lets it DM them without them changing any privacy
	// settings.
	scopes = "identify guilds.join"

	userAgent = "DiscordBot (https://github.com/SkyeKippen/eve-qs-industry, 1.0)"
)

// Config is the Discord application and bot the app uses.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	BotToken     string
	GuildID      string // the server linked users are added to
}

var envNames = []string{"DISCORD_CLIENT_ID", "DISCORD_CLIENT_SECRET", "DISCORD_CALLBACK_URL", "DISCORD_BOT_TOKEN", "DISCORD_GUILD_ID"}

// LoadConfigFromEnv reads the Discord settings from the environment. It
// returns nil and no error when none are set, so the app runs without
// Discord, and an error naming what is missing when only some are.
func LoadConfigFromEnv() (*Config, error) {
	values := make([]string, len(envNames))
	var missing []string
	for i, name := range envNames {
		values[i] = strings.TrimSpace(os.Getenv(name))
		if values[i] == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == len(envNames) {
		return nil, nil
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("discord: %s must be set too", strings.Join(missing, ", "))
	}
	return &Config{
		ClientID:     values[0],
		ClientSecret: values[1],
		RedirectURI:  values[2],
		BotToken:     values[3],
		GuildID:      values[4],
	}, nil
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// User is a Discord account.
type User struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

// DisplayName is the name Discord shows for the user.
func (u User) DisplayName() string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

// AuthorizeURL is where to send the user to sign in with Discord.
func (c *Config) AuthorizeURL(state string) string {
	q := url.Values{
		"response_type": {"code"},
		"client_id":     {c.ClientID},
		"scope":         {scopes},
		"state":         {state},
		"redirect_uri":  {c.RedirectURI},
	}
	return authorizeURL + "?" + q.Encode()
}

// ExchangeCode trades the code from the sign-in callback for an access
// token for the user. The app uses it once, to read the user and add them
// to the server, and does not keep it.
func (c *Config) ExchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {c.RedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.ClientID, c.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := do(req, &tok); err != nil {
		return "", fmt.Errorf("discord: exchanging code: %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("discord: token response had no access token")
	}
	return tok.AccessToken, nil
}

// CurrentUser returns the user an access token belongs to.
func CurrentUser(ctx context.Context, accessToken string) (User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/users/@me", nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	var user User
	if err := do(req, &user); err != nil {
		return User{}, fmt.Errorf("discord: reading user: %w", err)
	}
	return user, nil
}

// JoinGuild adds the user to the app's server using their access token, or
// does nothing when they are already in it. The bot needs the Create Invite
// permission there.
func (c *Config) JoinGuild(ctx context.Context, userID, accessToken string) error {
	req, err := c.botRequest(ctx, http.MethodPut, "/guilds/"+c.GuildID+"/members/"+userID,
		map[string]string{"access_token": accessToken})
	if err != nil {
		return err
	}
	if err := do(req, nil); err != nil {
		return fmt.Errorf("discord: adding user %s to server: %w", userID, err)
	}
	return nil
}

// SendDM sends the user a direct message from the bot. They must share a
// server with the bot and allow DMs from its members, which Discord does
// by default.
func (c *Config) SendDM(ctx context.Context, userID string, msg Message) error {
	req, err := c.botRequest(ctx, http.MethodPost, "/users/@me/channels", map[string]string{"recipient_id": userID})
	if err != nil {
		return err
	}
	var channel struct {
		ID string `json:"id"`
	}
	if err := do(req, &channel); err != nil {
		return fmt.Errorf("discord: opening DM with %s: %w", userID, err)
	}

	req, err = c.botRequest(ctx, http.MethodPost, "/channels/"+channel.ID+"/messages", msg)
	if err != nil {
		return err
	}
	if err := do(req, nil); err != nil {
		return fmt.Errorf("discord: sending DM to %s: %w", userID, err)
	}
	return nil
}

func (c *Config) botRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+c.BotToken)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// APIError is an error response from Discord.
type APIError struct {
	Status  int
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return fmt.Sprintf("HTTP %d: %s (code %d)", e.Status, e.Message, e.Code)
}

// Discord error codes the app reacts to.
const (
	CodeCannotDMUser = 50007 // the user blocked the bot or turned off DMs
)

// maxRateLimitWait is the longest do waits out a rate limit before giving up.
const maxRateLimitWait = 10 * time.Second

// do sends req, waiting out one rate limit, and decodes a successful JSON
// response into out when it isn't nil.
func do(req *http.Request, out any) error {
	req.Header.Set("User-Agent", userAgent)
	var body []byte
	if req.GetBody != nil {
		r, err := req.GetBody()
		if err != nil {
			return err
		}
		if body, err = io.ReadAll(r); err != nil {
			return err
		}
	}

	for attempt := 0; ; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			var limit struct {
				RetryAfter float64 `json:"retry_after"`
			}
			_ = json.Unmarshal(data, &limit)
			wait := time.Duration(limit.RetryAfter * float64(time.Second))
			if wait > 0 && wait <= maxRateLimitWait {
				select {
				case <-time.After(wait):
					continue
				case <-req.Context().Done():
					return req.Context().Err()
				}
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			apiErr := &APIError{Status: resp.StatusCode}
			_ = json.Unmarshal(data, apiErr)
			return apiErr
		}
		if out == nil || len(data) == 0 {
			return nil
		}
		return json.Unmarshal(data, out)
	}
}
