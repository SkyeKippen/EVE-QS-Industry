package esi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	AuthorizationURL = "https://login.eveonline.com/v2/oauth/authorize"
	TokenURL         = "https://login.eveonline.com/v2/oauth/token"
)

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
	UsePKCE      bool
	HTTPClient   *http.Client
}

func (c *Config) usesBasicAuth() bool {
	return c.ClientSecret != ""
}

func (c *Config) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

type PKCE struct {
	Verifier  string
	Challenge string
}

func GeneratePKCE() (PKCE, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return PKCE{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	return PKCE{Verifier: verifier, Challenge: challenge}, nil
}

func GenerateState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (c *Config) BuildAuthorizeURL(state string, pkce *PKCE) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("redirect_uri", c.RedirectURI)
	q.Set("client_id", c.ClientID)
	q.Set("scope", strings.Join(c.Scopes, " "))
	q.Set("state", state)
	if pkce != nil {
		q.Set("code_challenge", pkce.Challenge)
		q.Set("code_challenge_method", "S256")
	}

	u, _ := url.Parse(AuthorizationURL)
	u.RawQuery = q.Encode()
	return u.String()
}

type TokenResponse struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"-"`
}

type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (c *Config) ExchangeCode(ctx context.Context, code, verifier string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURI)
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	if !c.usesBasicAuth() {
		form.Set("client_id", c.ClientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "QS-Indy (contact: skyemeadows20@gmail.com)")

	if c.usesBasicAuth() {
		req.SetBasicAuth(c.ClientID, c.ClientSecret)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var tokErr tokenErrorResponse
		_ = json.Unmarshal(body, &tokErr)
		if tokErr.Error != "" {
			return nil, errors.New(tokErr.Error)
		}
		return nil, errors.New(tokErr.ErrorDescription)
	}
	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)

	return &tok, nil
}

func (c *Config) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	if !c.usesBasicAuth() {
		form.Set("client_id", c.ClientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "QS-Indy (contact: skyemeadows20@gmail.com)")
	if c.usesBasicAuth() {
		req.SetBasicAuth(c.ClientID, c.ClientSecret)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var tokErr tokenErrorResponse
		_ = json.Unmarshal(body, &tokErr)
		if tokErr.Error != "" {
			return nil, err
		}
		return nil, err
	}

	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)

	return &tok, nil
}
