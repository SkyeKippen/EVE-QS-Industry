package esi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
)

type TokenFile struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

func loadToken() (*oauth2.Token, error) {
	err := godotenv.Load("config/.env")
	if err != nil {
		log.Fatal("Error loading .env file", err)
	}

	existingToken, err := os.ReadFile("esi/token.json")
	if err != nil {
		return nil, err
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

	return os.WriteFile("esi/token.json", data, 0600)
}

func RefreshToken() (*oauth2.Token, error) {
	err := godotenv.Load("config/.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	oauthConfig := &oauth2.Config{
		ClientID:     os.Getenv("ESI_CLIENT_ID"),
		ClientSecret: os.Getenv("ESI_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("ESI_CALLBACK_URL"),
		Scopes:       strings.Split(os.Getenv("ESI_SCOPES"), " "),
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.eveonline.com/v2/oauth/authorize",
			TokenURL: "https://login.eveonline.com/v2/oauth/token",
		},
	}

	existingToken, err := loadToken()
	if err != nil {
		log.Fatal("Error loading token.json file", err)
	}

	tokenSource := oauthConfig.TokenSource(context.Background(), existingToken)

	newToken, err := tokenSource.Token()
	if err != nil {
		log.Fatal("Failed to refresh token:", err)
	}

	log.Println("Successfully refreshed token")

	if err := saveToken(newToken); err != nil {
		log.Println("Warning: could not save updated token:", err)
	} else {
		log.Println("Saved updated token to config/token.json")
	}

	return newToken, err
}
