package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/joho/godotenv"
)

type EsiToken struct {
	AccessToken  string    `json:"access_token"`
	ExpiresIn    int64     `json:"expires_in"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	url := oauthConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

var done = make(chan bool)

func handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")

	oauthToken, err := oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Expiry:", oauthToken.Expiry)

	token := EsiToken{
		AccessToken:  oauthToken.AccessToken,
		TokenType:    oauthToken.TokenType,
		RefreshToken: oauthToken.RefreshToken,
		ExpiresIn:    oauthToken.ExpiresIn,
		ExpiresAt:    oauthToken.Expiry,
	}

	err = saveToken(token)
	if err != nil {
		return
	}
}

func saveToken(token EsiToken) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile("esi/token.json", data, 0600)
	if err != nil {
		return err
	}

	return nil
}

func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

var oauthConfig *oauth2.Config

func main() {
	err := godotenv.Load("config/.env")
	if err != nil {
		slog.Error("Error loading .env file", err)
	} else {
		log.Println("Loading .env file")
		slog.Debug("Loaded .env file")
	}

	oauthConfig = &oauth2.Config{
		ClientID:     os.Getenv("ESI_CLIENT_ID"),
		ClientSecret: os.Getenv("ESI_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("ESI_CALLBACK_URL"),
		Scopes:       strings.Split(os.Getenv("ESI_SCOPES"), " "),
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.eveonline.com/v2/oauth/authorize",
			TokenURL: "https://login.eveonline.com/v2/oauth/token",
		},
	}

	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/callback", handleCallback)

	log.Println("Successfully setup http handlers")

	go http.ListenAndServe("localhost:8080", nil)

	fmt.Println("Need to get the token, none exists")
	url := oauthConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	log.Println("Opening browser with url:", url)
	err = openBrowser(url)
	if err != nil {
		log.Println("Browser error:", err)
	}

	log.Println("Waiting for login...")

	<-done

	log.Println("Login complete, continuing...")

}
