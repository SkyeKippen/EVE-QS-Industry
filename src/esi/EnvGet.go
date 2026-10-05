package esi

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

const EnvFile = "config/.env"

// LoadEnv loads EnvFile into the process environment. Variables that are
// already set are left untouched, so calling it more than once is harmless.
func LoadEnv() error {
	if err := godotenv.Load(EnvFile); err != nil {
		return fmt.Errorf("loading %s: %w", EnvFile, err)
	}
	return nil
}

func LoadConfigFromEnv() (*Config, error) {
	if err := LoadEnv(); err != nil {
		return nil, fmt.Errorf("evesso: %w", err)
	}

	clientID := os.Getenv("ESI_CLIENT_ID")
	if clientID == "" {
		return nil, fmt.Errorf("evesso: ESI_CLIENT_ID is required")
	}
	redirectURI := os.Getenv("ESI_CALLBACK_URL")
	if redirectURI == "" {
		return nil, fmt.Errorf("evesso: ESI_CALLBACK_URL is required")
	}
	rawScopes := os.Getenv("ESI_SCOPES")
	if rawScopes == "" {
		return nil, fmt.Errorf("evesso: ESI_SCOPES is required")
	}
	scopes := splitScopes(rawScopes)

	usePKCE := true
	if v := os.Getenv("EVE_USE_PKCE"); v != "" {
		usePKCE = strings.EqualFold(v, "true") || v == "1"
	}

	return &Config{
		ClientID:     clientID,
		ClientSecret: os.Getenv("ESI_CLIENT_SECRET"),
		RedirectURI:  redirectURI,
		Scopes:       scopes,
		UsePKCE:      usePKCE,
	}, nil
}

func splitScopes(raw string) []string {
	raw = strings.ReplaceAll(raw, ",", " ")
	fields := strings.Fields(raw)
	return fields
}
