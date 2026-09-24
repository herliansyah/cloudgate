package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Default fallback client IDs (users can override in UI)
const (
	DefaultGoogleClientID     = "cloudgate-client-id.apps.googleusercontent.com"
	DefaultGoogleClientSecret = "GOCSPX-dummysecret"

	DefaultOneDriveClientID = "cloudgate-onedrive-client-id"
	DefaultDropboxClientID  = "cloudgate-dropbox-app-key"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type ProviderConfig struct {
	AuthEndpoint  string
	TokenEndpoint string
	DefaultScopes []string
	DefaultID     string
	DefaultSecret string
}

var ProviderConfigs = map[string]ProviderConfig{
	"gdrive": {
		AuthEndpoint:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenEndpoint: "https://oauth2.googleapis.com/token",
		DefaultScopes: []string{
			"https://www.googleapis.com/auth/drive",
			"https://www.googleapis.com/auth/drive.metadata.readonly",
		},
		DefaultID:     DefaultGoogleClientID,
		DefaultSecret: DefaultGoogleClientSecret,
	},
	"google": {
		AuthEndpoint:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenEndpoint: "https://oauth2.googleapis.com/token",
		DefaultScopes: []string{
			"https://www.googleapis.com/auth/drive",
			"https://www.googleapis.com/auth/drive.metadata.readonly",
		},
		DefaultID:     DefaultGoogleClientID,
		DefaultSecret: DefaultGoogleClientSecret,
	},
	"onedrive": {
		AuthEndpoint:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenEndpoint: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		DefaultScopes: []string{
			"files.readwrite",
			"offline_access",
		},
		DefaultID: DefaultOneDriveClientID,
	},
	"dropbox": {
		AuthEndpoint:  "https://www.dropbox.com/oauth2/authorize",
		TokenEndpoint: "https://api.dropboxapi.com/oauth2/token",
		DefaultScopes: []string{"files.content.read", "files.content.write"},
		DefaultID:     DefaultDropboxClientID,
	},
}

// GenerateAuthURL builds the standard OAuth2 consent URL for the chosen provider.
func GenerateAuthURL(provider string, redirectURI string, customClientID string, state string) (string, error) {
	cfg, ok := ProviderConfigs[provider]
	if !ok {
		return "", fmt.Errorf("unsupported OAuth provider: %s", provider)
	}

	clientID := cfg.DefaultID
	if customClientID != "" {
		clientID = customClientID
	}

	vals := url.Values{}
	vals.Set("client_id", clientID)
	vals.Set("redirect_uri", redirectURI)
	vals.Set("response_type", "code")
	vals.Set("scope", strings.Join(cfg.DefaultScopes, " "))
	vals.Set("access_type", "offline")
	vals.Set("prompt", "consent")
	if state != "" {
		vals.Set("state", state)
	}

	return fmt.Sprintf("%s?%s", cfg.AuthEndpoint, vals.Encode()), nil
}

// ExchangeCode handles exchanging the authorization code for access and refresh tokens.
func ExchangeCode(ctx context.Context, provider, code, redirectURI, customClientID, customClientSecret string) (*TokenResponse, error) {
	cfg, ok := ProviderConfigs[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported OAuth provider: %s", provider)
	}

	clientID := cfg.DefaultID
	if customClientID != "" {
		clientID = customClientID
	}
	clientSecret := cfg.DefaultSecret
	if customClientSecret != "" {
		clientSecret = customClientSecret
	}

	vals := url.Values{}
	vals.Set("code", code)
	vals.Set("client_id", clientID)
	vals.Set("client_secret", clientSecret)
	vals.Set("redirect_uri", redirectURI)
	vals.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.TokenEndpoint, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed token exchange HTTP request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var token TokenResponse
	if err := json.Unmarshal(bodyBytes, &token); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &token, nil
}
