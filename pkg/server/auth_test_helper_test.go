package server_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"github.com/herliansyah/cloudgate/pkg/auth"
	"github.com/herliansyah/cloudgate/pkg/db"
)

func setupTestAuth(t *testing.T, database *db.DB, serverURL string) *http.Cookie {
	t.Helper()
	hash, err := auth.HashMasterPassword("testpassword123")
	if err != nil {
		t.Fatalf("failed to hash test password: %v", err)
	}
	if err := database.SetMasterPassword(hash); err != nil {
		t.Fatalf("failed to set test master password: %v", err)
	}
	token, err := auth.GenerateSessionToken()
	if err != nil {
		t.Fatalf("failed to generate session token: %v", err)
	}
	if err := database.CreateGatewaySession(token, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("failed to create gateway session: %v", err)
	}
	cookie := &http.Cookie{
		Name:  "cg_session",
		Value: token,
		Path:  "/",
	}
	if serverURL != "" {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("failed to create cookie jar: %v", err)
		}
		u, err := url.Parse(serverURL)
		if err != nil {
			t.Fatalf("failed to parse test server url: %v", err)
		}
		jar.SetCookies(u, []*http.Cookie{cookie})
		http.DefaultClient.Jar = jar
	}
	return cookie
}
