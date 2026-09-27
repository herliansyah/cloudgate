package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
)

func TestUpdaterAPI(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cg_updater_api_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := NewServer(database, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := ts.Client()

	// 1. GET /api/changelog should be accessible (whitelisted)
	resp, err := client.Get(ts.URL + "/api/changelog")
	if err != nil {
		t.Fatalf("failed to get changelog: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for changelog, got %d", resp.StatusCode)
	}
	var changelogData map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&changelogData); err != nil {
		t.Fatalf("failed to decode changelog response: %v", err)
	}
	if !strings.Contains(changelogData["changelog"], "Changelog") {
		t.Errorf("expected changelog content to mention Changelog, got: %s", changelogData["changelog"])
	}

	// 2. Setup GatewayAuth to verify protection on POST /api/updater/apply
	setupPayload := map[string]string{"password": "secretpassword"}
	setupBody, _ := json.Marshal(setupPayload)
	respSetup, err := client.Post(ts.URL+"/api/auth/gateway/setup", "application/json", bytes.NewReader(setupBody))
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	respSetup.Body.Close()

	// 3. POST /api/updater/apply without session should return 401 Unauthorized
	applyPayload := map[string]string{
		"download_url": "http://localhost/nonexistent",
		"checksum_url": "http://localhost/checksums.txt",
	}
	applyBody, _ := json.Marshal(applyPayload)
	reqUnauth, _ := http.NewRequest("POST", ts.URL+"/api/updater/apply", bytes.NewReader(applyBody))
	reqUnauth.Header.Set("Content-Type", "application/json")
	respUnauth, err := client.Do(reqUnauth)
	if err != nil {
		t.Fatalf("failed to send unauth apply request: %v", err)
	}
	respUnauth.Body.Close()

	if respUnauth.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for unauth /api/updater/apply, got %d", respUnauth.StatusCode)
	}

	// 4. Unlock GatewayAuth to get session cookie
	unlockPayload := map[string]string{"password": "secretpassword"}
	unlockBody, _ := json.Marshal(unlockPayload)
	respUnlock, err := client.Post(ts.URL+"/api/auth/gateway/unlock", "application/json", bytes.NewReader(unlockBody))
	if err != nil {
		t.Fatalf("unlock failed: %v", err)
	}
	var cookie *http.Cookie
	for _, c := range respUnlock.Cookies() {
		if c.Name == "cg_session" {
			cookie = c
			break
		}
	}
	respUnlock.Body.Close()
	if cookie == nil {
		t.Fatalf("expected cg_session cookie after unlock")
	}

	// 5. Authorized POST /api/updater/apply with invalid URLs should fail gracefully (returns 500 error, not 401)
	reqAuth, _ := http.NewRequest("POST", ts.URL+"/api/updater/apply", bytes.NewReader(applyBody))
	reqAuth.Header.Set("Content-Type", "application/json")
	reqAuth.AddCookie(cookie)

	respAuth, err := client.Do(reqAuth)
	if err != nil {
		t.Fatalf("apply request failed: %v", err)
	}
	defer respAuth.Body.Close()

	if respAuth.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 InternalServerError for unreachable update URLs, got %d", respAuth.StatusCode)
	}

	// 6. Test restart trigger hook
	restartCalled := false
	srv.SetRestartTrigger(func() {
		restartCalled = true
	})
	srv.triggerRestart()
	if !restartCalled {
		t.Errorf("expected restartTrigger to be called")
	}
}
