package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
)

func TestGatewayAuthFlow(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cg_gateway_test_*")
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

	// 1. Initial status: setup required, unauthenticated even initially
	resp, err := client.Get(ts.URL + "/api/auth/gateway/status")
	if err != nil {
		t.Fatalf("failed to get gateway status: %v", err)
	}
	var status struct {
		Enabled       bool `json:"enabled"`
		SetupRequired bool `json:"setup_required"`
		CanSetup      bool `json:"can_setup"`
		Authenticated bool `json:"authenticated"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()

	if status.Enabled {
		t.Errorf("expected GatewayAuth to be not enabled initially")
	}
	if !status.SetupRequired {
		t.Errorf("expected SetupRequired to be true initially")
	}
	if !status.CanSetup {
		t.Errorf("expected CanSetup to be true from loopback client")
	}
	if status.Authenticated {
		t.Errorf("expected authenticated to be false when uninitialized")
	}

	// 1b. Verify protected endpoints (/api/stats) are blocked before setup is performed
	preSetupResp, err := client.Get(ts.URL + "/api/stats")
	if err != nil {
		t.Fatalf("failed to call stats before setup: %v", err)
	}
	preSetupResp.Body.Close()
	if preSetupResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized before setup, got %d", preSetupResp.StatusCode)
	}

	// 1c. Verify non-loopback setup request is rejected with 403 Forbidden
	rec := httptest.NewRecorder()
	remoteReq := httptest.NewRequest("POST", "/api/auth/gateway/setup", bytes.NewBufferString(`{"password":"testpassword123"}`))
	remoteReq.RemoteAddr = "192.168.1.100:45678"
	srv.Handler().ServeHTTP(rec, remoteReq)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for remote setup attempt, got %d", rec.Code)
	}

	// 2. Setup MasterPassword from loopback
	setupBody := bytes.NewBufferString(`{"password":"testpassword123"}`)
	resp, err = client.Post(ts.URL+"/api/auth/gateway/setup", "application/json", setupBody)
	if err != nil {
		t.Fatalf("failed to setup master password: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on setup, got %d", resp.StatusCode)
	}
	// Extract session cookie
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "cg_session" {
			sessionCookie = c
			break
		}
	}
	resp.Body.Close()
	if sessionCookie == nil {
		t.Fatalf("expected cg_session cookie to be returned upon setup")
	}

	// 3. Request without cookie should be rejected with 401 on protected endpoint (/api/stats)
	noAuthClient := &http.Client{}
	resp, err = noAuthClient.Get(ts.URL + "/api/stats")
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for request without session, got %d", resp.StatusCode)
	}

	// 4. Request with cookie should succeed
	req, _ := http.NewRequest("GET", ts.URL+"/api/stats", nil)
	req.AddCookie(sessionCookie)
	resp, err = noAuthClient.Do(req)
	if err != nil {
		t.Fatalf("failed to get stats with cookie: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for request with session cookie, got %d", resp.StatusCode)
	}

	// 5. Test unlock with wrong password
	unlockBodyWrong := bytes.NewBufferString(`{"password":"wrongpassword"}`)
	resp, err = noAuthClient.Post(ts.URL+"/api/auth/gateway/unlock", "application/json", unlockBodyWrong)
	if err != nil {
		t.Fatalf("failed to call unlock: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for wrong password, got %d", resp.StatusCode)
	}

	// 6. Test unlock with correct password
	unlockBodyRight := bytes.NewBufferString(`{"password":"testpassword123"}`)
	resp, err = noAuthClient.Post(ts.URL+"/api/auth/gateway/unlock", "application/json", unlockBodyRight)
	if err != nil {
		t.Fatalf("failed to call unlock: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for correct unlock password, got %d", resp.StatusCode)
	}
	var newCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "cg_session" {
			newCookie = c
			break
		}
	}
	resp.Body.Close()
	if newCookie == nil {
		t.Fatalf("expected cg_session cookie on unlock")
	}

	// 7. Test lock
	reqLock, _ := http.NewRequest("POST", ts.URL+"/api/auth/gateway/lock", nil)
	reqLock.AddCookie(newCookie)
	resp, err = noAuthClient.Do(reqLock)
	if err != nil {
		t.Fatalf("failed to call lock: %v", err)
	}
	resp.Body.Close()

	// After lock, the old cookie should be invalid
	reqAfterLock, _ := http.NewRequest("GET", ts.URL+"/api/stats", nil)
	reqAfterLock.AddCookie(newCookie)
	resp, err = noAuthClient.Do(reqAfterLock)
	if err != nil {
		t.Fatalf("failed to test stats after lock: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized after lock, got %d", resp.StatusCode)
	}
}
