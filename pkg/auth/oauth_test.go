package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPKCE(t *testing.T) {
	v, c, err := NewPKCE()
	if err != nil || len(v) < 43 {
		t.Fatalf("bad verifier %q %v", v, err)
	}
	sum := sha256.Sum256([]byte(v))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != c {
		t.Fatal("challenge is not S256(verifier)")
	}
}

func TestPCloudTokenEndpoint(t *testing.T) {
	if e, _ := PCloudTokenEndpoint("eapi.pcloud.com"); e != "https://eapi.pcloud.com/oauth2_token" {
		t.Fatal(e)
	}
	if e, _ := PCloudTokenEndpoint(""); e != "https://api.pcloud.com/oauth2_token" {
		t.Fatal(e)
	}
	if _, err := PCloudTokenEndpoint("attacker.example.com"); err == nil {
		t.Fatal("expected error")
	}
}

func TestGenerateAuthURLRequiresClientID(t *testing.T) {
	if _, err := GenerateAuthURL("dropbox", "http://localhost/cb", "", "s", ""); err == nil {
		t.Fatal("expected error without client id")
	}
}

func TestExchangeCodeErrorPayload(t *testing.T) {
	old := HTTPClient
	defer func() { HTTPClient = old }()
	HTTPClient = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		// pCloud-style: HTTP 200 with an error body.
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"result":2012,"error":"Invalid 'code' provided."}`)), Header: http.Header{}}, nil
	})}
	_, err := ExchangeCode(context.Background(), ExchangeRequest{Provider: "pcloud", Code: "c", ClientID: "a", ClientSecret: "b"})
	if err == nil || !strings.Contains(err.Error(), "Invalid 'code'") {
		t.Fatalf("expected provider error, got %v", err)
	}
}
