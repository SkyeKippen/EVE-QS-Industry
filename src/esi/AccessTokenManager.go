package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/oauth2"
)

const tokenFilePath = "esi/token.json"

type TokenFile struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

func loadToken() (*oauth2.Token, error) {
	existingToken, err := os.ReadFile(tokenFilePath)
	if err != nil {
		return nil, fmt.Errorf("could not read token file: %w", err)
	}

	var tf TokenFile
	if err := json.Unmarshal(existingToken, &tf); err != nil {
		return nil, fmt.Errorf("could not parse token file: %w", err)
	}

	if !tf.Expiry.IsZero() && time.Now().Before(tf.Expiry) {
		log.Println("Access token still valid, skipping refresh")
		return &oauth2.Token{
			AccessToken:  tf.AccessToken,
			RefreshToken: tf.RefreshToken,
			Expiry:       tf.Expiry,
		}, nil
	}

	return &oauth2.Token{
		RefreshToken: tf.RefreshToken,
	}, nil
}

func saveToken(token *oauth2.Token) error {
	tf := TokenFile{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}

	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal token: %w", err)
	}

	return os.WriteFile(tokenFilePath, data, 0600)
}

func RefreshToken() (*oauth2.Token, error) {
	if err := LoadEnv(); err != nil {
		return nil, err
	}

	oauthConfig := &oauth2.Config{
		ClientID:     os.Getenv("ESI_CLIENT_ID"),
		ClientSecret: os.Getenv("ESI_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("ESI_CALLBACK_URL"),
		Scopes:       splitScopes(os.Getenv("ESI_SCOPES")),
		Endpoint: oauth2.Endpoint{
			AuthURL:  AuthorizationURL,
			TokenURL: TokenURL,
		},
	}

	existingToken, err := loadToken()
	if err != nil {
		return nil, err
	}

	tokenSource := oauthConfig.TokenSource(context.Background(), existingToken)

	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	log.Println("Successfully refreshed token")

	if err := saveToken(newToken); err != nil {
		log.Println("Warning: could not save updated token:", err)
	} else {
		log.Println("Saved updated token to", tokenFilePath)
	}

	return newToken, nil
}
