package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/auth"
	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/storage"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func memoryFactory(provider, id string, creds map[string]string, opts ...storage.AdapterOption) (storage.Driver, error) {
	return storage.NewRcloneDriverFromCreds(provider, id, creds, append(opts, storage.WithMemoryBackend())...)
}

func TestOAuthCallbackSuccessFlow(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "cg_oauth_cb_*")
	defer os.RemoveAll(tempDir)
	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	srv := NewServer(database, nil)
	defer srv.Close()
	srv.SetDriverFactory(memoryFactory)
	if err := srv.LoadAccounts(); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	tokenCalls := map[string]url.Values{}
	oldAuth, oldID := auth.HTTPClient, identityHTTPClient
	auth.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		mu.Lock()
		tokenCalls[r.URL.Host] = r.PostForm
		mu.Unlock()
		return jsonResponse(200, `{"access_token":"AT-`+r.URL.Host+`","refresh_token":"RT","token_type":"bearer","expires_in":3600}`), nil
	})}
	identityHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "eapi.pcloud.com":
			return jsonResponse(200, `{"result":0,"email":"eu@pcloud.test"}`), nil
		case "api.dropboxapi.com":
			return jsonResponse(200, `{"email":"db@dropbox.test","name":{"display_name":"DB"}}`), nil
		}
		return jsonResponse(404, `{}`), nil
	})}
	defer func() { auth.HTTPClient, identityHTTPClient = oldAuth, oldID }()

	// pCloud EU: token exchange must use eapi.pcloud.com and persist hostname.
	state, _ := srv.putOAuthState(pendingOAuth{Provider: "pcloud", ClientID: "cid", ClientSecret: "sec", RedirectURI: "http://localhost:5210/api/auth/pcloud/callback", UIOrigin: "http://192.168.1.5:5210"})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/auth/pcloud/callback?code=CODE&state="+state+"&hostname=eapi.pcloud.com&locationid=2", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("pcloud callback failed: %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"http://192.168.1.5:5210"`) || strings.Contains(rr.Body.String(), "'*'") {
		t.Fatalf("postMessage must target the UI origin: %s", rr.Body.String())
	}
	if _, ok := tokenCalls["eapi.pcloud.com"]; !ok {
		t.Fatalf("expected EU token endpoint, got %v", tokenCalls)
	}
	accs, _ := database.GetAccounts()
	if len(accs) != 1 || accs[0].Provider != "pcloud" || accs[0].Email != "eu@pcloud.test" {
		t.Fatalf("unexpected accounts %+v", accs)
	}
	var creds map[string]string
	_ = json.Unmarshal([]byte(accs[0].Credentials), &creds)
	if creds["hostname"] != "eapi.pcloud.com" || !strings.Contains(creds["token"], `"refresh_token":"RT"`) || !strings.Contains(creds["token"], `"expiry"`) {
		t.Fatalf("unexpected creds %+v", creds)
	}
	// State is single-use.
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/auth/pcloud/callback?code=CODE&state="+state, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("replayed state must be rejected, got %d", rr.Code)
	}

	// Dropbox: PKCE verifier must accompany the code exchange.
	state, _ = srv.putOAuthState(pendingOAuth{Provider: "dropbox", ClientID: "cid", ClientSecret: "sec", RedirectURI: "http://localhost/api/auth/dropbox/callback", CodeVerifier: "VERIFIER", Name: `<img src=x onerror=alert(1)>`, UIOrigin: "http://localhost"})
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/auth/dropbox/callback?code=CODE2&state="+state, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("dropbox callback failed: %d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "<img src=x") {
		t.Fatal("account name must be HTML-escaped")
	}
	if tokenCalls["api.dropboxapi.com"].Get("code_verifier") != "VERIFIER" {
		t.Fatalf("missing code_verifier: %v", tokenCalls["api.dropboxapi.com"])
	}
}
