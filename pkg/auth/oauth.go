package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// TokenResponse is the provider's reply to the authorization-code exchange.
type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ProviderConfig describes the OAuth 2.0 endpoints of a provider.
type ProviderConfig struct {
	AuthEndpoint  string
	TokenEndpoint string
	Scopes        []string
	// AuthParams are provider-specific authorize parameters (e.g. offline access).
	AuthParams map[string]string
	// PKCE enables RFC 7636 S256 code challenges (in addition to the client secret).
	PKCE bool
}

var googleConfig = ProviderConfig{
	AuthEndpoint:  "https://accounts.google.com/o/oauth2/v2/auth",
	TokenEndpoint: "https://oauth2.googleapis.com/token",
	Scopes:        []string{"https://www.googleapis.com/auth/drive"},
	// Google only issues a refresh token with access_type=offline; prompt=consent
	// guarantees one is returned on re-authorisation too.
	AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
	PKCE:       true,
}

// ProviderConfigs lists every provider connected through OAuth.
var ProviderConfigs = map[string]ProviderConfig{
	"gdrive": googleConfig,
	"google": googleConfig,
	"onedrive": {
		AuthEndpoint:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenEndpoint: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		Scopes:        []string{"Files.ReadWrite", "offline_access", "User.Read"},
		PKCE:          true,
	},
	"dropbox": {
		AuthEndpoint:  "https://www.dropbox.com/oauth2/authorize",
		TokenEndpoint: "https://api.dropboxapi.com/oauth2/token",
		Scopes: []string{
			"account_info.read",
			"files.metadata.read", "files.metadata.write",
			"files.content.read", "files.content.write",
		},
		// Without token_access_type=offline Dropbox only issues a ~4h token and no refresh token.
		AuthParams: map[string]string{"token_access_type": "offline"},
		PKCE:       true,
	},
	"box": {
		AuthEndpoint:  "https://account.box.com/api/oauth2/authorize",
		TokenEndpoint: "https://api.box.com/oauth2/token",
		Scopes:        []string{"root_readwrite"},
	},
	"pcloud": {
		AuthEndpoint: "https://my.pcloud.com/oauth2/authorize",
		// The real token host depends on the account region; see PCloudTokenEndpoint.
		TokenEndpoint: "https://api.pcloud.com/oauth2_token",
	},
	"yandex": {
		AuthEndpoint:  "https://oauth.yandex.com/authorize",
		TokenEndpoint: "https://oauth.yandex.com/token",
		// Scopes are defined on the registered Yandex app (cloud_api:disk.*).
		AuthParams: map[string]string{"force_confirm": "yes"},
	},
}

// PCloudTokenEndpoint returns the token endpoint for the pCloud API host
// reported in the authorisation redirect (api.pcloud.com for US accounts,
// eapi.pcloud.com for EU accounts).
func PCloudTokenEndpoint(hostname string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(hostname)) {
	case "", "api.pcloud.com":
		return "https://api.pcloud.com/oauth2_token", nil
	case "eapi.pcloud.com":
		return "https://eapi.pcloud.com/oauth2_token", nil
	}
	return "", fmt.Errorf("unexpected pCloud API host %q", hostname)
}

// NewPKCE returns a random code verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string, err error) {
	var b [48]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b[:])
	return verifier, oauth2.S256ChallengeFromVerifier(verifier), nil
}

// SupportsPKCE reports whether PKCE is used for provider.
func SupportsPKCE(provider string) bool {
	cfg, ok := ProviderConfigs[provider]
	return ok && cfg.PKCE
}

// GenerateAuthURL builds the OAuth consent URL for provider.
func GenerateAuthURL(provider, redirectURI, clientID, state, codeChallenge string) (string, error) {
	cfg, ok := ProviderConfigs[provider]
	if !ok {
		return "", fmt.Errorf("unsupported OAuth provider: %s", provider)
	}
	if strings.TrimSpace(clientID) == "" {
		return "", fmt.Errorf("OAuth Client ID is required for %s", provider)
	}
	vals := url.Values{}
	vals.Set("client_id", clientID)
	vals.Set("redirect_uri", redirectURI)
	vals.Set("response_type", "code")
	if len(cfg.Scopes) > 0 {
		vals.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	for k, v := range cfg.AuthParams {
		vals.Set(k, v)
	}
	if state != "" {
		vals.Set("state", state)
	}
	if cfg.PKCE && codeChallenge != "" {
		vals.Set("code_challenge", codeChallenge)
		vals.Set("code_challenge_method", "S256")
	}
	return cfg.AuthEndpoint + "?" + vals.Encode(), nil
}

// ExchangeRequest holds the parameters of an authorization-code exchange.
type ExchangeRequest struct {
	Provider      string
	Code          string
	RedirectURI   string
	ClientID      string
	ClientSecret  string
	CodeVerifier  string
	TokenEndpoint string // optional override (validated by caller), e.g. pCloud EU
}

// HTTPClient performs token exchanges; replaceable in tests.
var HTTPClient = &http.Client{Timeout: 20 * time.Second}

// ExchangeCode exchanges an authorization code for tokens.
func ExchangeCode(ctx context.Context, req ExchangeRequest) (*TokenResponse, error) {
	cfg, ok := ProviderConfigs[req.Provider]
	if !ok {
		return nil, fmt.Errorf("unsupported OAuth provider: %s", req.Provider)
	}
	if req.ClientID == "" || req.ClientSecret == "" {
		return nil, fmt.Errorf("provider %s requires a valid Client ID and Client Secret", req.Provider)
	}
	endpoint := cfg.TokenEndpoint
	if req.TokenEndpoint != "" {
		endpoint = req.TokenEndpoint
	}

	vals := url.Values{}
	vals.Set("code", req.Code)
	vals.Set("client_id", req.ClientID)
	vals.Set("client_secret", req.ClientSecret)
	vals.Set("redirect_uri", req.RedirectURI)
	vals.Set("grant_type", "authorization_code")
	if req.CodeVerifier != "" {
		vals.Set("code_verifier", req.CodeVerifier)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}
	// Some providers (pCloud) answer HTTP 200 with an error payload.
	if token.AccessToken == "" {
		msg := token.ErrorDescription
		if msg == "" {
			msg = token.Error
		}
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return nil, fmt.Errorf("token exchange returned no access token: %s", msg)
	}
	return &token, nil
}
