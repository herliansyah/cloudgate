package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/storage"
)

// DriverFactory builds a storage driver for an account from its credentials.
// Tests replace it to run against rclone's memory backend.
type DriverFactory func(provider, accountID string, creds map[string]string, opts ...storage.AdapterOption) (storage.Driver, error)

func defaultDriverFactory(provider, accountID string, creds map[string]string, opts ...storage.AdapterOption) (storage.Driver, error) {
	return storage.NewRcloneDriverFromCreds(provider, accountID, creds, opts...)
}

// SetDriverFactory overrides how drivers are constructed (used by tests).
func (s *Server) SetDriverFactory(f DriverFactory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f == nil {
		f = defaultDriverFactory
	}
	s.driverFactory = f
}

const (
	connectTimeout = 90 * time.Second // MEGA/Proton logins can be slow
	statsTimeout   = 10 * time.Second
)

// buildDriver constructs a driver. When persist is true, rotated credentials
// (OAuth refresh, sessions, host keys) are written back to the database.
func (s *Server) buildDriver(provider, accountID string, creds map[string]string, email, name string, persist bool) (storage.Driver, error) {
	s.mu.RLock()
	factory := s.driverFactory
	s.mu.RUnlock()
	opts := []storage.AdapterOption{storage.WithIdentity(email, name)}
	if persist {
		opts = append(opts, storage.WithPersister(s.persistCredentials))
	}
	return factory(provider, accountID, creds, opts...)
}

// persistCredentials saves rotated credentials for an existing account.
func (s *Server) persistCredentials(accountID string, creds map[string]string) {
	b, err := json.Marshal(creds)
	if err != nil {
		return
	}
	// The account may not exist yet while it is being connected; the caller
	// saves the adapter's credential snapshot afterwards in that case.
	_ = s.database.UpdateAccountCredentials(accountID, string(b))
}

func credentialsOf(d storage.Driver, fallback map[string]string) map[string]string {
	if c, ok := d.(interface{ Credentials() map[string]string }); ok {
		return c.Credentials()
	}
	return fallback
}

func parseCredentials(raw string) (map[string]string, error) {
	creds := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return creds, nil
	}
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return nil, fmt.Errorf("stored credentials are corrupt: %w", err)
	}
	return creds, nil
}

// driverForAccount restores the driver of a saved account. Accounts that
// cannot be restored get an UnavailableDriver so failures are visible.
func (s *Server) driverForAccount(acc db.RemoteAccount) storage.Driver {
	provider := string(storage.NormalizeProvider(acc.Provider))
	if !storage.IsSupportedProvider(provider) {
		return storage.NewUnavailableDriver(acc.ID, acc.Provider, fmt.Errorf("unsupported provider %q", acc.Provider))
	}
	creds, err := parseCredentials(acc.Credentials)
	if err != nil {
		return storage.NewUnavailableDriver(acc.ID, provider, err)
	}
	if err := storage.ValidateCredentials(provider, creds); err != nil {
		return storage.NewUnavailableDriver(acc.ID, provider, fmt.Errorf("%v - reconnect this account", err))
	}
	d, err := s.buildDriver(provider, acc.ID, creds, acc.Email, acc.Name, true)
	if err != nil {
		return storage.NewUnavailableDriver(acc.ID, provider, err)
	}
	return d
}

// LoadAccounts restores drivers for all saved accounts and builds the
// default round-robin pool from the enabled ones.
func (s *Server) LoadAccounts() error {
	accounts, err := s.database.GetAccounts()
	if err != nil {
		return err
	}
	var enabled []storage.Driver
	for _, acc := range accounts {
		d := s.driverForAccount(acc)
		s.RegisterDriver(d)
		if acc.Enabled || acc.Status != "disabled" {
			enabled = append(enabled, d)
		}
	}
	s.RegisterPool(storage.NewStoragePool("all_pool", "Round-Robin All Drives", enabled))
	return nil
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	type AccountStat struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		Provider   string     `json:"provider"`
		Status     string     `json:"status"`
		Error      string     `json:"error,omitempty"`
		Enabled    bool       `json:"enabled"`
		Email      string     `json:"email,omitempty"`
		LastSyncAt *time.Time `json:"last_sync_at,omitempty"`
		Total      int64      `json:"total"`
		Used       int64      `json:"used"`
		Free       int64      `json:"free"`
	}

	s.mu.RLock()
	drivers := make(map[string]storage.Driver, len(s.drivers))
	for k, v := range s.drivers {
		drivers[k] = v
	}
	s.mu.RUnlock()

	dbAccounts, _ := s.database.GetAccounts()
	dbMap := make(map[string]db.RemoteAccount)
	for _, a := range dbAccounts {
		dbMap[a.ID] = a
	}

	type result struct {
		quota storage.QuotaInfo
		err   error
	}
	results := make(map[string]result, len(drivers))
	var wg sync.WaitGroup
	var rmu sync.Mutex
	for id, driver := range drivers {
		wg.Add(1)
		go func(id string, d storage.Driver) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), statsTimeout)
			defer cancel()
			q, err := d.About(ctx)
			rmu.Lock()
			results[id] = result{q, err}
			rmu.Unlock()
		}(id, driver)
	}
	wg.Wait()

	var totalStorage, totalUsed, totalFree int64
	accountStats := []AccountStat{}
	for id, driver := range drivers {
		accInfo := dbMap[id]
		name := id
		if accInfo.Name != "" {
			name = accInfo.Name
		}
		res := results[id]
		quota := res.quota
		status := "connected"
		errMsg := ""
		if !accInfo.Enabled && accInfo.Status == "disabled" {
			status = "disabled"
		} else if res.err != nil {
			status = "error"
			errMsg = res.err.Error()
			if accInfo.QuotaTotal > 0 {
				free := accInfo.QuotaTotal - accInfo.QuotaUsed
				if free < 0 {
					free = 0
				}
				quota = storage.QuotaInfo{Total: accInfo.QuotaTotal, Used: accInfo.QuotaUsed, Free: free}
			}
		} else if storage.IsUnmetered(quota) && accInfo.QuotaTotal > 0 && accInfo.QuotaTotal != storage.UnmeteredQuota {
			// No provider quota: show the allocation configured for the account.
			free := accInfo.QuotaTotal - quota.Used
			if free < 0 {
				free = 0
			}
			quota = storage.QuotaInfo{Total: accInfo.QuotaTotal, Used: quota.Used, Free: free}
		}
		if status != "error" || quota.Total > 0 {
			totalStorage += quota.Total
			totalUsed += quota.Used
			totalFree += quota.Free
		}
		accountStats = append(accountStats, AccountStat{
			ID:         id,
			Name:       name,
			Provider:   driver.Provider(),
			Status:     status,
			Error:      errMsg,
			Enabled:    accInfo.Enabled,
			Email:      accInfo.Email,
			LastSyncAt: accInfo.LastSyncAt,
			Total:      quota.Total,
			Used:       quota.Used,
			Free:       quota.Free,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_storage": totalStorage,
		"total_used":    totalUsed,
		"total_free":    totalFree,
		"accounts":      accountStats,
	})
}

// accountPrincipal derives the human identity shown for an account.
func accountPrincipal(provider string, extra map[string]string) string {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(extra[k]); v != "" {
				return v
			}
		}
		return ""
	}
	switch provider {
	case "s3":
		if b := get("bucket"); b != "" {
			if ep := get("endpoint"); ep != "" {
				return fmt.Sprintf("%s (%s)", b, ep)
			}
			return b
		}
	case "webdav", "koofr":
		user := get("username", "user", "email")
		if u, err := url.Parse(get("url")); err == nil && u.Host != "" && user != "" {
			return fmt.Sprintf("%s@%s", user, u.Host)
		}
		return user
	case "mega", "filen", "pikpak":
		return get("email", "user", "username")
	case "b2":
		return get("bucket", "bucket_name", "account", "key_id", "username")
	case "sftp":
		host, user, port := get("host"), get("user", "username"), get("port")
		if port == "" {
			port = "22"
		}
		switch {
		case user != "" && host != "":
			return fmt.Sprintf("%s@%s:%s", user, host, port)
		case host != "":
			return host
		}
		return user
	case "smb":
		host, share, user := get("host"), get("share"), get("user", "username")
		switch {
		case user != "" && host != "" && share != "":
			return fmt.Sprintf("%s@%s/%s", user, host, share)
		case host != "" && share != "":
			return fmt.Sprintf("%s/%s", host, share)
		case host != "":
			return host
		}
		return user
	case "protondrive":
		return get("username", "user", "email")
	}
	return ""
}

// credentialFields extracts string credential fields from a JSON payload.
func credentialFields(raw map[string]any, skip ...string) map[string]string {
	skipSet := map[string]bool{}
	for _, k := range skip {
		skipSet[k] = true
	}
	out := make(map[string]string)
	for k, v := range raw {
		if skipSet[k] || strings.HasPrefix(k, "rclone.") {
			continue
		}
		switch x := v.(type) {
		case string:
			if x != "" {
				out[k] = x
			}
		case float64:
			out[k] = fmt.Sprintf("%v", x)
		case bool:
			out[k] = fmt.Sprintf("%t", x)
		}
	}
	return out
}

func newAccountID(provider string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%d_%s", provider, time.Now().Unix(), hex.EncodeToString(b[:]))
}

func (s *Server) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	str := func(k string) string {
		v, _ := raw[k].(string)
		return strings.TrimSpace(v)
	}
	provider := string(storage.NormalizeProvider(str("provider")))
	if provider == "" {
		writeError(w, http.StatusBadRequest, "provider required")
		return
	}
	if !storage.IsSupportedProvider(provider) {
		writeError(w, http.StatusBadRequest, "unsupported provider: "+provider)
		return
	}
	if storage.IsOAuthProvider(storage.Provider(provider)) {
		writeError(w, http.StatusBadRequest, providerDisplayName(provider)+" must be connected through the OAuth sign-in flow")
		return
	}
	name := str("name")
	rootFolder := str("root_folder")
	if rootFolder == "" {
		rootFolder = "/"
	}
	var quotaOverride int64
	if v, ok := raw["quota_total"].(float64); ok && v > 0 {
		quotaOverride = int64(v)
	}

	creds := credentialFields(raw, "id", "provider", "name", "root_folder", "quota_total")
	if err := storage.ValidateCredentials(provider, creds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	principal := accountPrincipal(provider, creds)
	email := principal
	if name == "" {
		if principal != "" {
			name = fmt.Sprintf("%s (%s)", providerDisplayName(provider), principal)
		} else {
			name = fmt.Sprintf("%s Account", providerDisplayName(provider))
		}
	}

	id := newAccountID(provider)
	drv, err := s.buildDriver(provider, id, creds, email, name, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Verify the credentials before the account is ever reported as connected.
	ctx, cancel := context.WithTimeout(r.Context(), connectTimeout)
	defer cancel()
	start := time.Now()
	if err := drv.TestConnection(ctx); err != nil {
		err = explainConnectErr(err)
		_ = s.database.RecordAudit("connect", name, id, err.Error(), "failed", time.Since(start).Milliseconds())
		writeError(w, http.StatusUnprocessableEntity, "Connection test failed: "+err.Error())
		return
	}
	quota, _ := drv.About(ctx)
	quotaTotal, quotaUsed := quota.Total, quota.Used
	// A user allocation only applies when the provider reports no real quota.
	if quotaOverride > 0 && storage.IsUnmetered(quota) {
		quotaTotal = quotaOverride
	}

	credsJSON, _ := json.Marshal(credentialsOf(drv, creds))
	acc := db.RemoteAccount{
		ID:          id,
		Provider:    provider,
		Name:        name,
		RootFolder:  rootFolder,
		Status:      "connected",
		Enabled:     true,
		QuotaTotal:  quotaTotal,
		QuotaUsed:   quotaUsed,
		Credentials: string(credsJSON),
		Email:       email,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := s.database.SaveAccount(acc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.registerConnected(drv)
	_ = s.database.RecordAudit("connect", acc.Name, acc.ID, "Account connected", "success", time.Since(start).Milliseconds())
	writeJSON(w, http.StatusCreated, redactAccount(acc))
}

// redactAccount strips stored secrets (passwords, tokens, keys) before an
// account is returned to the browser. The vault export keeps them.
func redactAccount(acc db.RemoteAccount) db.RemoteAccount {
	acc.Credentials = ""
	return acc
}

func redactAccounts(accs []db.RemoteAccount) []db.RemoteAccount {
	out := make([]db.RemoteAccount, len(accs))
	for i, a := range accs {
		out[i] = redactAccount(a)
	}
	return out
}

// explainConnectErr makes timeouts actionable (e.g. Proton answers 429 with a
// one-hour Retry-After that the client silently waits out).
func explainConnectErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %v waiting for the provider: check network/firewall, or the provider may be rate-limiting this IP (wait and try again later)", connectTimeout)
	}
	return err
}

// registerConnected adds a freshly connected driver to the registry and pool.
func (s *Server) registerConnected(drv storage.Driver) {
	s.mu.Lock()
	s.drivers[drv.ID()] = drv
	if allPool, ok := s.pools["all_pool"]; ok {
		allPool.AddDriver(drv)
	}
	s.mu.Unlock()
}

func (s *Server) handleTestConnectionNew(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}
	provider, _ := raw["provider"].(string)
	provider = string(storage.NormalizeProvider(provider))
	if provider == "" {
		writeError(w, http.StatusBadRequest, "provider required")
		return
	}
	fail := func(err error, durationMs int64) {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error(), "duration_ms": durationMs})
	}
	if !storage.IsSupportedProvider(provider) {
		fail(fmt.Errorf("unsupported provider: %s", provider), 0)
		return
	}
	creds := credentialFields(raw, "id", "provider", "name", "root_folder", "quota_total")
	if err := storage.ValidateCredentials(provider, creds); err != nil {
		fail(err, 0)
		return
	}
	drv, err := s.buildDriver(provider, "test_"+newAccountID(provider), creds, "", "Test Connection", false)
	if err != nil {
		fail(err, 0)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), connectTimeout)
	defer cancel()
	start := time.Now()
	err = drv.TestConnection(ctx)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		fail(explainConnectErr(err), durationMs)
		return
	}
	quota, _ := drv.About(ctx)
	resp := map[string]any{"success": true, "duration_ms": durationMs, "quota": quota}
	if fp, ok := drv.(interface{ HostKeyFingerprint() string }); ok && fp.HostKeyFingerprint() != "" {
		resp["host_key_fingerprint"] = fp.HostKeyFingerprint()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetShareLink(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	filePath := r.URL.Query().Get("path")
	wantPublic := r.URL.Query().Get("public") == "1" || r.URL.Query().Get("public") == "true"
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "path required")
		return
	}

	s.mu.RLock()
	var drv storage.Driver
	if accountID != "" {
		drv = s.drivers[accountID]
	} else {
		for _, d := range s.drivers {
			drv = d
			break
		}
	}
	s.mu.RUnlock()
	if drv == nil {
		writeError(w, http.StatusNotFound, "no storage driver available")
		return
	}

	if wantPublic {
		pl, ok := drv.(interface {
			PublicLink(context.Context, string) (string, error)
		})
		if !ok {
			writeError(w, http.StatusBadRequest, "public links are not supported for this account")
			return
		}
		link, err := pl.PublicLink(r.Context(), filePath)
		if err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, storage.ErrFileNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err.Error())
			return
		}
		_ = s.database.RecordAudit("share", filePath, drv.ID(), "Public link created", "success", 0)
		writeJSON(w, http.StatusOK, map[string]any{
			"share_url":     link,
			"path":          filePath,
			"account_id":    drv.ID(),
			"public":        true,
			"requires_auth": false,
		})
		return
	}

	link, err := drv.GetShareLink(r.Context(), filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fullURL := link
	if strings.HasPrefix(link, "/") {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		fullURL = fmt.Sprintf("%s://%s%s", scheme, r.Host, link)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"share_url":     fullURL,
		"path":          filePath,
		"account_id":    drv.ID(),
		"public":        false,
		"requires_auth": true,
	})
}
