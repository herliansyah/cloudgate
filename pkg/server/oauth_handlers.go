package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/herliansyah/cloudgate/pkg/auth"
	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/storage"
)

const (
	oauthStateTTL       = 15 * time.Minute
	maxPendingOAuthFlow = 64
)

// pendingOAuth is the server-side half of an OAuth flow. Only the random
// state key travels through the browser; secrets never leave the server.
type pendingOAuth struct {
	Provider     string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Name         string
	CodeVerifier string
	UIOrigin     string
	Created      time.Time
}

func (s *Server) putOAuthState(p pendingOAuth) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	key := hex.EncodeToString(b[:])
	p.Created = time.Now()
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	for k, v := range s.oauthStates {
		if time.Since(v.Created) > oauthStateTTL {
			delete(s.oauthStates, k)
		}
	}
	if len(s.oauthStates) >= maxPendingOAuthFlow {
		var oldestKey string
		var oldest time.Time
		for k, v := range s.oauthStates {
			if oldestKey == "" || v.Created.Before(oldest) {
				oldestKey, oldest = k, v.Created
			}
		}
		delete(s.oauthStates, oldestKey)
	}
	s.oauthStates[key] = p
	return key, nil
}

// takeOAuthState returns and deletes the pending flow (single use).
func (s *Server) takeOAuthState(key string) (pendingOAuth, bool) {
	if key == "" {
		return pendingOAuth{}, false
	}
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()
	p, ok := s.oauthStates[key]
	if !ok {
		return pendingOAuth{}, false
	}
	delete(s.oauthStates, key)
	if time.Since(p.Created) > oauthStateTTL {
		return pendingOAuth{}, false
	}
	return p, true
}

func isPrivateIP(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback()
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "https"
	}
	return "http"
}

// requestHost returns the host the browser used, honouring X-Forwarded-Host
// set by a reverse proxy (first value only).
func requestHost(r *http.Request) string {
	if fh := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); fh != "" {
		return fh
	}
	return r.Host
}

func callbackProviderName(provider string) string {
	if provider == "gdrive" {
		return "google"
	}
	return provider
}

// defaultRedirectURI computes the callback URL registered with the provider.
func defaultRedirectURI(r *http.Request, provider string) string {
	host := requestHost(r)
	// Google rejects private LAN IPs in redirect URIs; use localhost instead.
	if (provider == "google" || provider == "gdrive") && isPrivateIP(host) {
		if _, port, err := net.SplitHostPort(host); err == nil {
			host = "localhost:" + port
		} else {
			host = "localhost"
		}
	}
	return fmt.Sprintf("%s://%s/api/auth/%s/callback", requestScheme(r), host, callbackProviderName(provider))
}

func validRedirectURI(raw, provider string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	return u.Path == "/api/auth/"+callbackProviderName(provider)+"/callback"
}

func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if _, ok := auth.ProviderConfigs[provider]; !ok {
		writeError(w, http.StatusBadRequest, "unsupported OAuth provider: "+provider)
		return
	}
	var req struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RedirectURI  string `json:"redirect_uri"`
		Name         string `json:"name"`
	}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid request payload")
			return
		}
	} else {
		q := r.URL.Query()
		req.ClientID, req.ClientSecret = q.Get("client_id"), q.Get("client_secret")
		req.RedirectURI, req.Name = q.Get("redirect_uri"), q.Get("name")
	}
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.ClientSecret = strings.TrimSpace(req.ClientSecret)

	redirectURI := defaultRedirectURI(r, provider)
	if req.RedirectURI != "" {
		if !validRedirectURI(req.RedirectURI, provider) {
			writeError(w, http.StatusBadRequest, "redirect_uri must point to /api/auth/"+callbackProviderName(provider)+"/callback")
			return
		}
		redirectURI = req.RedirectURI
	}

	// Preview mode (no client ID yet): only report the redirect URI to register.
	if req.ClientID == "" {
		writeJSON(w, http.StatusOK, map[string]string{"redirect_uri": redirectURI})
		return
	}
	if req.ClientSecret == "" {
		writeError(w, http.StatusBadRequest, "client_secret is required")
		return
	}

	pending := pendingOAuth{
		Provider:     provider,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		RedirectURI:  redirectURI,
		Name:         strings.TrimSpace(req.Name),
		UIOrigin:     fmt.Sprintf("%s://%s", requestScheme(r), requestHost(r)),
	}
	challenge := ""
	if auth.SupportsPKCE(provider) {
		verifier, ch, err := auth.NewPKCE()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create PKCE challenge")
			return
		}
		pending.CodeVerifier, challenge = verifier, ch
	}
	state, err := s.putOAuthState(pending)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create OAuth state")
		return
	}
	authURL, err := auth.GenerateAuthURL(provider, redirectURI, req.ClientID, state, challenge)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.URL.Query().Get("redirect") == "true" {
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"auth_url":     authURL,
		"redirect_uri": redirectURI,
	})
}

func oauthProviderTitle(provider string) string {
	switch provider {
	case "gdrive", "google":
		return "Google Drive"
	case "onedrive":
		return "Microsoft OneDrive"
	}
	return providerDisplayName(provider)
}

var oauthPageTmpl = template.Must(template.New("oauth").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title>
<style>body{font-family:-apple-system,sans-serif;background:#0f172a;color:#f8fafc;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;text-align:center}
.card{background:#1e293b;padding:36px;border-radius:16px;border:1px solid {{if .OK}}#334155{{else}}#ef4444{{end}};max-width:560px}
h2{color:{{if .OK}}#10b981{{else}}#ef4444{{end}};margin-bottom:12px}p{color:#cbd5e1;line-height:1.5}
.err{background:rgba(239,68,68,.15);color:#fca5a5;padding:12px;border-radius:8px;font-family:monospace;font-size:.85rem;margin:16px 0;word-break:break-all}
button{background:#334155;color:#fff;border:0;padding:10px 24px;border-radius:8px;cursor:pointer}</style>
</head>
<body><div class="card">
<h2>{{.Title}}</h2>
<p>{{.Message}}</p>
{{if .Detail}}<div class="err">{{.Detail}}</div>{{end}}
{{if .Hint}}<p style="font-size:.85rem;color:#94a3b8">{{.Hint}}</p>{{end}}
<button type="button" onclick="window.close()">Close window / Tutup jendela</button>
</div>
<script>
(function(){
  var msg = {{.Message2}};
  var origin = {{.Origin}};
  if (window.opener && msg && origin) {
    try { window.opener.postMessage(msg, origin); } catch (e) {}
  }
  {{if .OK}}setTimeout(function(){ window.close(); }, 1500);{{end}}
})();
</script>
</body></html>`))

type oauthPage struct {
	OK       bool
	Title    string
	Message  string
	Detail   string
	Hint     string
	Message2 map[string]any
	Origin   string
}

func renderOAuthPage(w http.ResponseWriter, status int, page oauthPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_ = oauthPageTmpl.Execute(w, page)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	pathProvider := r.PathValue("provider")
	title := oauthProviderTitle(pathProvider)
	q := r.URL.Query()
	pending, ok := s.takeOAuthState(q.Get("state"))

	fail := func(detail, hint string) {
		page := oauthPage{
			Title:   title + ": authentication failed / autentikasi gagal",
			Message: "Cloudgate could not complete the " + title + " OAuth sign-in.",
			Detail:  detail,
			Hint:    hint,
		}
		if ok && pending.UIOrigin != "" {
			page.Message2 = map[string]any{"type": "oauth_error", "provider": pathProvider, "error": detail}
			page.Origin = pending.UIOrigin
		}
		renderOAuthPage(w, http.StatusBadRequest, page)
	}

	if !ok {
		fail("Invalid or expired OAuth state. Start the connection again from the Cloudgate dashboard.", "")
		return
	}
	if callbackProviderName(pending.Provider) != callbackProviderName(pathProvider) {
		fail("OAuth state does not belong to this provider.", "")
		return
	}
	if e := q.Get("error"); e != "" {
		desc := q.Get("error_description")
		if desc != "" {
			e = e + ": " + desc
		}
		fail(e, "")
		return
	}
	code := q.Get("code")
	if code == "" {
		fail("Missing authorization code.", "")
		return
	}

	provider := string(storage.NormalizeProvider(pending.Provider))
	exchange := auth.ExchangeRequest{
		Provider:     pending.Provider,
		Code:         code,
		RedirectURI:  pending.RedirectURI,
		ClientID:     pending.ClientID,
		ClientSecret: pending.ClientSecret,
		CodeVerifier: pending.CodeVerifier,
	}
	pcloudHost := ""
	if provider == string(storage.ProviderPCloud) {
		pcloudHost = strings.ToLower(q.Get("hostname"))
		if pcloudHost == "" {
			pcloudHost = "api.pcloud.com"
		}
		endpoint, err := auth.PCloudTokenEndpoint(pcloudHost)
		if err != nil {
			fail(err.Error(), "")
			return
		}
		exchange.TokenEndpoint = endpoint
	}

	ctx, cancel := context.WithTimeout(r.Context(), connectTimeout)
	defer cancel()
	tokenResp, err := auth.ExchangeCode(ctx, exchange)
	if err != nil {
		fail(err.Error(), "Check that the "+title+" OAuth Client ID, Client Secret and redirect URI match the app you registered.")
		return
	}

	userEmail, userName := fetchOAuthIdentity(ctx, provider, tokenResp.AccessToken, pcloudHost)
	accName := pending.Name
	if accName == "" {
		switch {
		case userName != "" && userEmail != "" && userName != userEmail:
			accName = fmt.Sprintf("%s (%s)", providerDisplayName(provider), userEmail)
		case userEmail != "":
			accName = fmt.Sprintf("%s (%s)", providerDisplayName(provider), userEmail)
		default:
			accName = fmt.Sprintf("%s Account", providerDisplayName(provider))
		}
	}

	creds := map[string]string{
		"client_id":     pending.ClientID,
		"client_secret": pending.ClientSecret,
		"token":         storage.OAuthTokenJSON(tokenResp.AccessToken, tokenResp.RefreshToken, tokenResp.TokenType, tokenResp.ExpiresIn),
		"access_token":  tokenResp.AccessToken,
	}
	if tokenResp.RefreshToken != "" {
		creds["refresh_token"] = tokenResp.RefreshToken
	}
	if pcloudHost != "" {
		creds["hostname"] = pcloudHost
	}

	accID := newAccountID(provider)
	drv, err := s.buildDriver(provider, accID, creds, userEmail, userName, true)
	if err != nil {
		fail(err.Error(), "")
		return
	}
	quota, err := drv.About(ctx)
	if err != nil {
		fail(err.Error(), "The token was issued but Cloudgate could not access the storage. Check the scopes/permissions of your app.")
		return
	}

	credsJSON, _ := json.Marshal(credentialsOf(drv, creds))
	acc := db.RemoteAccount{
		ID:          accID,
		Provider:    provider,
		Name:        accName,
		RootFolder:  "/",
		Status:      "connected",
		Enabled:     true,
		QuotaTotal:  quota.Total,
		QuotaUsed:   quota.Used,
		Credentials: string(credsJSON),
		Email:       userEmail,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := s.database.SaveAccount(acc); err != nil {
		fail("failed to save account: "+err.Error(), "")
		return
	}
	s.registerConnected(drv)
	_ = s.database.RecordAudit("connect", acc.Name, acc.ID, "Connected via OAuth", "success", 0)

	go func(d storage.Driver) {
		ictx, icancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer icancel()
		files, err := d.List(ictx, "/")
		if err != nil {
			return
		}
		indexed := make([]db.IndexedFile, 0, len(files))
		for _, f := range files {
			indexed = append(indexed, db.IndexedFile{AccountID: accID, Path: f.Path, Name: f.Name, Size: f.Size, IsDir: f.IsDir, ModTime: f.ModTime})
		}
		_ = s.database.IndexFiles(indexed)
	}(drv)

	renderOAuthPage(w, http.StatusOK, oauthPage{
		OK:       true,
		Title:    "✓ " + title + " connected",
		Message:  "Account " + accName + " has been connected to Cloudgate. This window will close automatically.",
		Message2: map[string]any{"type": "oauth_complete", "provider": provider, "account_id": acc.ID},
		Origin:   pending.UIOrigin,
	})
}

// identityHTTPClient is replaceable in tests.
var identityHTTPClient = &http.Client{Timeout: 15 * time.Second}

// fetchOAuthIdentity best-effort resolves the account email and display name.
func fetchOAuthIdentity(ctx context.Context, provider, accessToken, pcloudHost string) (email, name string) {
	getJSON := func(method, rawURL, authHeader string, out any) bool {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
		if err != nil {
			return false
		}
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		resp, err := identityHTTPClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out) == nil
	}
	bearer := "Bearer " + accessToken
	switch provider {
	case "gdrive":
		var about struct {
			User struct {
				DisplayName  string `json:"displayName"`
				EmailAddress string `json:"emailAddress"`
			} `json:"user"`
		}
		if getJSON("GET", "https://www.googleapis.com/drive/v3/about?fields=user", bearer, &about) {
			return about.User.EmailAddress, about.User.DisplayName
		}
	case "onedrive":
		var me struct {
			DisplayName string `json:"displayName"`
			Mail        string `json:"mail"`
			UPN         string `json:"userPrincipalName"`
		}
		if getJSON("GET", "https://graph.microsoft.com/v1.0/me", bearer, &me) {
			if me.Mail == "" {
				me.Mail = me.UPN
			}
			return me.Mail, me.DisplayName
		}
	case "dropbox":
		var acct struct {
			Name struct {
				DisplayName string `json:"display_name"`
			} `json:"name"`
			Email string `json:"email"`
		}
		if getJSON("POST", "https://api.dropboxapi.com/2/users/get_current_account", bearer, &acct) {
			return acct.Email, acct.Name.DisplayName
		}
	case "box":
		var me struct {
			Name  string `json:"name"`
			Login string `json:"login"`
		}
		if getJSON("GET", "https://api.box.com/2.0/users/me", bearer, &me) {
			return me.Login, me.Name
		}
	case "pcloud":
		host := pcloudHost
		if host == "" {
			host = "api.pcloud.com"
		}
		var info struct {
			Result int    `json:"result"`
			Email  string `json:"email"`
		}
		if getJSON("GET", "https://"+host+"/userinfo", bearer, &info) && info.Result == 0 {
			return info.Email, info.Email
		}
	case "yandex":
		var disk struct {
			User struct {
				Login       string `json:"login"`
				DisplayName string `json:"display_name"`
			} `json:"user"`
		}
		if getJSON("GET", "https://cloud-api.yandex.net/v1/disk", "OAuth "+accessToken, &disk) {
			return disk.User.Login, disk.User.DisplayName
		}
	}
	return "", ""
}
