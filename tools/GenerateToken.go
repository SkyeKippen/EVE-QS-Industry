package main

import (
	"QS-Indy/src/esi"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"time"

	"golang.org/x/oauth2"
)

const tokenFilePath = "esi/token.json"

var (
	oauthConfig *oauth2.Config
	state       string

	// receives the outcome of the callback; buffered so a repeat callback never blocks
	done = make(chan error, 1)
)

func handleLogin(w http.ResponseWriter, r *http.Request) {
	url := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	err := completeLogin(r)
	if err != nil {
		log.Println("Login failed:", err)
		http.Error(w, "Login failed: "+err.Error(), http.StatusBadRequest)
	} else {
		fmt.Fprintln(w, "Token saved to "+tokenFilePath+". You can close this tab.")
	}

	select {
	case done <- err:
	default:
	}
}

func completeLogin(r *http.Request) error {
	q := r.URL.Query()

	if ssoErr := q.Get("error"); ssoErr != "" {
		return fmt.Errorf("EVE SSO returned %s: %s", ssoErr, q.Get("error_description"))
	}

	if q.Get("state") != state {
		return errors.New("state mismatch, ignoring callback")
	}

	code := q.Get("code")
	if code == "" {
		return errors.New("callback is missing the code parameter")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	oauthToken, err := oauthConfig.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	log.Println("Expiry:", oauthToken.Expiry)

	if err := saveToken(oauthToken); err != nil {
		return fmt.Errorf("could not save token: %w", err)
	}

	return nil
}

// saveToken writes the token in the format esi.RefreshToken reads.
func saveToken(token *oauth2.Token) error {
	data, err := json.MarshalIndent(esi.TokenFile{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(tokenFilePath, data, 0600)
}

func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func main() {
	cfg, err := esi.LoadConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	slog.Debug("Loaded .env file")

	oauthConfig = &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Scopes:       cfg.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  esi.AuthorizationURL,
			TokenURL: esi.TokenURL,
		},
	}

	state, err = esi.GenerateState()
	if err != nil {
		log.Fatal("Could not generate state: ", err)
	}

	// serve the callback on the same host, port and path EVE SSO redirects to
	callbackUrl, err := neturl.Parse(oauthConfig.RedirectURL)
	if err != nil || callbackUrl.Host == "" || callbackUrl.Path == "" {
		log.Fatal("ESI_CALLBACK_URL is not a valid URL: ", oauthConfig.RedirectURL)
	}

	http.HandleFunc("/login", handleLogin)
	http.HandleFunc(callbackUrl.Path, handleCallback)

	// claim the port before opening the browser so a clash fails here, not after login
	listener, err := net.Listen("tcp", callbackUrl.Host)
	if err != nil {
		log.Fatalf("Could not listen on %s (is the web app running on the same port?): %v", callbackUrl.Host, err)
	}

	server := &http.Server{}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("Callback server stopped: ", err)
		}
	}()

	log.Println("Listening for the callback on", oauthConfig.RedirectURL)

	url := oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	log.Println("Opening browser with url:", url)
	if err := openBrowser(url); err != nil {
		log.Println("Browser error:", err)
	}

	log.Println("Waiting for login...")

	loginErr := <-done

	// let the browser receive its response page before exiting
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)

	if loginErr != nil {
		log.Fatal("Login failed: ", loginErr)
	}

	log.Println("Login complete, token saved to", tokenFilePath)
}
