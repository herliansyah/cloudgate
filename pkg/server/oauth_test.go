package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/server"
)

// TestGoogleOAuthFlow verifies that initiating a Google Drive connection
// produces a valid Google OAuth authorization URL with required scopes.
func TestGoogleOAuthFlow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloudgate_oauth_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := server.NewServer(database, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// 1. Request OAuth authorization URL for Google Drive
	resp, err := http.Get(ts.URL + "/api/auth/google/login")
	if err != nil {
		t.Fatalf("failed to request google oauth login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 OK from /api/auth/google/login, got %d", resp.StatusCode)
	}

	var res map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	authURL := res["auth_url"]
	if !strings.HasPrefix(authURL, "https://accounts.google.com/o/oauth2/v2/auth") {
		t.Fatalf("expected Google OAuth URL, got: %s", authURL)
	}

	if !strings.Contains(authURL, "scope=") || !strings.Contains(authURL, "redirect_uri=") {
		t.Fatalf("auth URL missing scope or redirect_uri parameters: %s", authURL)
	}
}

// TestGoogleOAuthPrivateIPNormalization verifies that private IP hosts (e.g. 192.168.x.x)
// are automatically normalized to localhost for Google compliance to prevent Google Error 400.
func TestGoogleOAuthPrivateIPNormalization(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloudgate_oauth_ip_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := server.NewServer(database, nil)

	req, _ := http.NewRequest("GET", "/api/auth/google/login", nil)
	req.Host = "192.168.7.8:5210" // Simulating access from LAN IP

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	var res map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&res)

	// The redirect URI must not contain 192.168.7.8 (which triggers Google 400),
	// but rather localhost:5210
	if strings.Contains(res["auth_url"], "192.168.7.8") {
		t.Fatalf("Google OAuth URL contains private IP which causes Google Error 400: %s", res["auth_url"])
	}
	if !strings.Contains(res["auth_url"], "localhost%3A5210") && !strings.Contains(res["auth_url"], "localhost:5210") {
		t.Fatalf("expected localhost:5210 in Google OAuth redirect_uri: %s", res["auth_url"])
	}
}

// TestRedirectURIConsistency verifies that gdrive and google both use the canonical /api/auth/google/callback
func TestRedirectURIConsistency(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "cloudgate_redir_*")
	defer os.RemoveAll(tempDir)
	database, _ := db.Open(tempDir)
	defer database.Close()
	srv := server.NewServer(database, nil)

	req, _ := http.NewRequest("GET", "/api/auth/gdrive/login", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	var res map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&res)

	// Must be canonical /api/auth/google/callback
	if !strings.HasSuffix(res["redirect_uri"], "/api/auth/google/callback") {
		t.Fatalf("expected canonical /api/auth/google/callback, got: %s", res["redirect_uri"])
	}
}


