package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/server"
)

func newOAuthTestServer(t *testing.T) (*server.Server, *httptest.Server) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "cloudgate_oauth_test_*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })
	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	srv := server.NewServer(database, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	setupTestAuth(t, database, ts.URL)
	return srv, ts
}

func oauthLogin(t *testing.T, base, provider string) (authURL *url.URL, redirectURI string) {
	t.Helper()
	resp, err := http.Get(base + "/api/auth/" + provider + "/login?client_id=my-client&client_secret=my-secret&name=Mine")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("[%s] expected 200, got %d", provider, resp.StatusCode)
	}
	var res map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&res)
	u, err := url.Parse(res["auth_url"])
	if err != nil || res["auth_url"] == "" {
		t.Fatalf("[%s] invalid auth_url %q", provider, res["auth_url"])
	}
	return u, res["redirect_uri"]
}

func TestOAuthLoginURLs(t *testing.T) {
	_, ts := newOAuthTestServer(t)
	cases := []struct {
		provider, prefix string
		wantParams       map[string]string
		pkce             bool
	}{
		{"google", "https://accounts.google.com/o/oauth2/v2/auth", map[string]string{"access_type": "offline", "prompt": "consent", "scope": "https://www.googleapis.com/auth/drive"}, true},
		{"onedrive", "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", map[string]string{"scope": "Files.ReadWrite offline_access User.Read"}, true},
		{"dropbox", "https://www.dropbox.com/oauth2/authorize", map[string]string{"token_access_type": "offline"}, true},
		{"box", "https://account.box.com/api/oauth2/authorize", map[string]string{"scope": "root_readwrite"}, false},
		{"pcloud", "https://my.pcloud.com/oauth2/authorize", nil, false},
		{"yandex", "https://oauth.yandex.com/authorize", nil, false},
	}
	for _, c := range cases {
		u, redirect := oauthLogin(t, ts.URL, c.provider)
		if !strings.HasPrefix(u.String(), c.prefix) {
			t.Errorf("[%s] unexpected auth url %s", c.provider, u)
		}
		q := u.Query()
		if q.Get("client_id") != "my-client" || q.Get("redirect_uri") != redirect || q.Get("response_type") != "code" {
			t.Errorf("[%s] missing basic params: %s", c.provider, u)
		}
		for k, v := range c.wantParams {
			if q.Get(k) != v {
				t.Errorf("[%s] expected %s=%q, got %q", c.provider, k, v, q.Get(k))
			}
		}
		if c.provider != "google" && q.Get("access_type") != "" {
			t.Errorf("[%s] Google-only access_type must not be sent", c.provider)
		}
		if (q.Get("code_challenge") != "") != c.pkce || (c.pkce && q.Get("code_challenge_method") != "S256") {
			t.Errorf("[%s] unexpected PKCE params: %s", c.provider, u)
		}
		state := q.Get("state")
		if len(state) != 64 || strings.Contains(u.String(), "my-secret") {
			t.Errorf("[%s] state must be an opaque nonce and the secret must not leak: %s", c.provider, u)
		}
	}
	// Koofr is not an OAuth provider.
	resp, _ := http.Get(ts.URL + "/api/auth/koofr/login?client_id=a&client_secret=b")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for koofr oauth, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestOAuthLoginPreviewReturnsRedirectOnly(t *testing.T) {
	_, ts := newOAuthTestServer(t)
	resp, err := http.Get(ts.URL + "/api/auth/dropbox/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var res map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if res["auth_url"] != "" || !strings.HasSuffix(res["redirect_uri"], "/api/auth/dropbox/callback") {
		t.Fatalf("unexpected preview response %v", res)
	}
}

func TestOAuthLoginPOST(t *testing.T) {
	_, ts := newOAuthTestServer(t)
	body := strings.NewReader(`{"client_id":"cid","client_secret":"sec","name":"x"}`)
	resp, err := http.Post(ts.URL+"/api/auth/box/login", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var res map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if !strings.Contains(res["auth_url"], "client_id=cid") {
		t.Fatalf("unexpected response %v", res)
	}
}

// TestGoogleOAuthPrivateIPNormalization verifies that private IP hosts are
// normalized to localhost for Google, which rejects LAN IPs in redirect URIs.
func TestGoogleOAuthPrivateIPNormalization(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "cloudgate_oauth_ip_test_*")
	defer os.RemoveAll(tempDir)
	database, _ := db.Open(tempDir)
	defer database.Close()
	srv := server.NewServer(database, nil)
	authCookie := setupTestAuth(t, database, "")

	req, _ := http.NewRequest("GET", "/api/auth/google/login?client_id=c&client_secret=s", nil)
	req.AddCookie(authCookie)
	req.Host = "192.168.7.8:5210"
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}
	var res map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&res)
	if strings.Contains(res["auth_url"], "192.168.7.8") {
		t.Fatalf("Google OAuth URL contains private IP: %s", res["auth_url"])
	}
	if res["redirect_uri"] != "http://localhost:5210/api/auth/google/callback" {
		t.Fatalf("unexpected redirect_uri %s", res["redirect_uri"])
	}
}

// TestRedirectURIConsistency verifies gdrive and google share /api/auth/google/callback.
func TestRedirectURIConsistency(t *testing.T) {
	_, ts := newOAuthTestServer(t)
	for _, p := range []string{"gdrive", "google"} {
		_, redirect := oauthLogin(t, ts.URL, p)
		if !strings.HasSuffix(redirect, "/api/auth/google/callback") {
			t.Fatalf("[%s] expected canonical google callback, got %s", p, redirect)
		}
	}
}

func TestOAuthLoginRejectsForeignRedirectURI(t *testing.T) {
	_, ts := newOAuthTestServer(t)
	resp, _ := http.Get(ts.URL + "/api/auth/box/login?client_id=a&client_secret=b&redirect_uri=" + url.QueryEscape("https://evil.example.com/steal"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestOAuthCallbackRejectsMissingOrForgedState(t *testing.T) {
	srv, _ := newOAuthTestServer(t)
	for _, q := range []string{"code=abc", "code=abc&state=forged", "error=%3Cscript%3Ealert(1)%3C/script%3E"} {
		req, _ := http.NewRequest("GET", "/api/auth/onedrive/callback?"+q, nil)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		body := rr.Body.String()
		if rr.Code != http.StatusBadRequest {
			t.Errorf("[%s] expected 400, got %d", q, rr.Code)
		}
		if !strings.Contains(body, "Microsoft OneDrive") || !strings.Contains(body, "Invalid or expired OAuth state") {
			t.Errorf("[%s] unexpected body: %s", q, body)
		}
		if strings.Contains(body, "<script>alert(1)") {
			t.Errorf("[%s] reflected XSS in callback page", q)
		}
	}
}

func TestOAuthRedirectHonoursForwardedHost(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "cloudgate_fwd_*")
	defer os.RemoveAll(tempDir)
	database, _ := db.Open(tempDir)
	defer database.Close()
	srv := server.NewServer(database, nil)
	cookie := setupTestAuth(t, database, "")
	req, _ := http.NewRequest("GET", "/api/auth/box/login", nil)
	req.AddCookie(cookie)
	req.Host = "127.0.0.1:5210"
	req.Header.Set("X-Forwarded-Host", "files.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var res map[string]string
	_ = json.NewDecoder(rr.Body).Decode(&res)
	if res["redirect_uri"] != "https://files.example.com/api/auth/box/callback" {
		t.Fatalf("unexpected redirect_uri %q", res["redirect_uri"])
	}
}
