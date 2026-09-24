package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herliansyah/cloudgate/pkg/auth"
	"github.com/herliansyah/cloudgate/pkg/config"
	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/storage"
	ghsync "github.com/herliansyah/cloudgate/pkg/sync"
	"github.com/herliansyah/cloudgate/pkg/updater"
)

type Server struct {
	mu           sync.RWMutex
	database     *db.DB
	trashManager *storage.TrashManager
	drivers      map[string]storage.Driver
	pools        map[string]*storage.StoragePool
	assets       fs.FS
}

func NewServer(database *db.DB, assets fs.FS) *Server {
	s := &Server{
		database:     database,
		trashManager: storage.NewTrashManager(database),
		drivers:      make(map[string]storage.Driver),
		pools:        make(map[string]*storage.StoragePool),
		assets:       assets,
	}
	_ = database.EnableFTSIndex()
	return s
}

// RegisterDriver registers a live driver into the server registry.
func (s *Server) RegisterDriver(d storage.Driver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drivers[d.ID()] = d
}

// RegisterPool registers a storage pool into the server registry.
func (s *Server) RegisterPool(p *storage.StoragePool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pools[p.ID()] = p
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API Routes
	mux.HandleFunc("GET /api/info", s.handleInfo)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/accounts", s.handleGetAccounts)
	mux.HandleFunc("POST /api/accounts", s.handleAddAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", s.handleDeleteAccount)

	// OAuth Routes
	mux.HandleFunc("GET /api/auth/{provider}/login", s.handleOAuthLogin)
	mux.HandleFunc("GET /api/auth/{provider}/callback", s.handleOAuthCallback)

	mux.HandleFunc("GET /api/pools", s.handleGetPools)

	mux.HandleFunc("GET /api/files", s.handleListFiles)
	mux.HandleFunc("POST /api/files/upload", s.handleUploadFile)
	mux.HandleFunc("GET /api/files/download", s.handleDownloadFile)
	mux.HandleFunc("POST /api/files/copy", s.handleCopyFile)
	mux.HandleFunc("POST /api/files/move", s.handleMoveFile)
	mux.HandleFunc("POST /api/files/trash", s.handleTrashFile)

	mux.HandleFunc("GET /api/trash", s.handleGetTrash)
	mux.HandleFunc("POST /api/trash/restore", s.handleRestoreTrash)
	mux.HandleFunc("DELETE /api/trash", s.handleEmptyTrash)

	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/audit", s.handleGetAudit)
	mux.HandleFunc("GET /api/audit/export", s.handleExportAudit)

	mux.HandleFunc("GET /api/updater/check", s.handleCheckUpdate)
	mux.HandleFunc("POST /api/sync/github/push", s.handleSyncPush)
	mux.HandleFunc("POST /api/sync/github/pull", s.handleSyncPull)

	// Embedded Static Assets
	if s.assets != nil {
		fileServer := http.FileServer(http.FS(s.assets))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<html><body><h1>%s v%s</h1><p>Author: %s</p><p><a href='%s'>%s</a></p><p>Web UI will be loaded here.</p></body></html>",
				config.AppName, config.AppVersion, config.AppAuthor, config.AppRepo, config.AppRepo)
		})
	}

	return s.corsMiddleware(mux)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"app":        config.AppName,
		"version":    config.AppVersion,
		"author":     config.AppAuthor,
		"repository": config.AppRepo,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var totalStorage, totalUsed, totalFree int64
	type AccountStat struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Provider string `json:"provider"`
		Status   string `json:"status"`
		Total    int64  `json:"total"`
		Used     int64  `json:"used"`
		Free     int64  `json:"free"`
	}

	var accountStats []AccountStat
	for id, driver := range s.drivers {
		quota, err := driver.About(r.Context())
		status := "connected"
		if err != nil {
			status = "error"
		} else {
			totalStorage += quota.Total
			totalUsed += quota.Used
			totalFree += quota.Free
		}
		accountStats = append(accountStats, AccountStat{
			ID:       id,
			Name:     id,
			Provider: driver.Provider(),
			Status:   status,
			Total:    quota.Total,
			Used:     quota.Used,
			Free:     quota.Free,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_storage": totalStorage,
		"total_used":    totalUsed,
		"total_free":    totalFree,
		"accounts":      accountStats,
	})
}

func (s *Server) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.database.GetAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) handleAddAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		Provider   string `json:"provider"`
		Name       string `json:"name"`
		RootFolder string `json:"root_folder"`
		QuotaTotal int64  `json:"quota_total"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	acc := db.RemoteAccount{
		ID:         req.ID,
		Provider:   req.Provider,
		Name:       req.Name,
		RootFolder: req.RootFolder,
		Status:     "connected",
		QuotaTotal: req.QuotaTotal,
		QuotaUsed:  0,
		UpdatedAt:  time.Now().UTC(),
	}

	if err := s.database.SaveAccount(acc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Register simulated mem driver if not present
	s.mu.Lock()
	if _, exists := s.drivers[acc.ID]; !exists {
		quota := acc.QuotaTotal
		if quota <= 0 {
			quota = 15 * 1024 * 1024 * 1024 // 15 GB default
		}
		s.drivers[acc.ID] = storage.NewMemDriver(acc.ID, acc.Provider, quota)
	}
	s.mu.Unlock()

	_ = s.database.RecordAudit("connect", acc.Name, acc.ID, "Account connected", "success", 0)
	writeJSON(w, http.StatusCreated, acc)
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing account id")
		return
	}

	s.mu.Lock()
	delete(s.drivers, id)
	for _, p := range s.pools {
		p.RemoveDriver(id)
	}
	s.mu.Unlock()

	_ = s.database.DeleteAccount(id)
	_ = s.database.ClearAccountIndex(id)
	_ = s.database.RecordAudit("disconnect", id, id, "Account disconnected", "success", 0)
	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if provider == "" {
		writeError(w, http.StatusBadRequest, "missing provider")
		return
	}

	customClientID := r.URL.Query().Get("client_id")
	customClientSecret := r.URL.Query().Get("client_secret")
	customRedirectURI := r.URL.Query().Get("redirect_uri")

	var redirectURI string
	if customRedirectURI != "" {
		redirectURI = customRedirectURI
	} else {
		host := r.Host
		// Google explicitly rejects private LAN IP addresses (192.168.x.x, 10.x.x.x) in redirect_uri.
		// Normalize private IP host to localhost for Google compliance.
		if (provider == "google" || provider == "gdrive") && isPrivateIP(host) {
			_, port, err := net.SplitHostPort(host)
			if err == nil {
				host = "localhost:" + port
			} else {
				host = "localhost"
			}
		}

		callbackProvider := provider
		if callbackProvider == "gdrive" {
			callbackProvider = "google"
		}

		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		redirectURI = fmt.Sprintf("%s://%s/api/auth/%s/callback", scheme, host, callbackProvider)
	}

	statePayload, _ := json.Marshal(map[string]string{
		"client_id":     customClientID,
		"client_secret": customClientSecret,
		"redirect_uri":  redirectURI,
		"provider":      provider,
	})
	stateStr := base64.RawURLEncoding.EncodeToString(statePayload)

	authURL, err := auth.GenerateAuthURL(provider, redirectURI, customClientID, stateStr)
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

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	code := r.URL.Query().Get("code")
	errParam := r.URL.Query().Get("error")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if errParam != "" {
		fmt.Fprintf(w, "<html><body style='font-family:sans-serif;padding:30px;background:#0f172a;color:#f8fafc;'><h2>Authentication Cancelled</h2><p>%s</p><button onclick='window.close()' style='padding:10px 20px;cursor:pointer;'>Close Window</button></body></html>", errParam)
		return
	}

	if code == "" {
		fmt.Fprint(w, "<html><body style='font-family:sans-serif;padding:30px;background:#0f172a;color:#f8fafc;'><h2>Missing Authorization Code</h2><button onclick='window.close()' style='padding:10px 20px;cursor:pointer;'>Close Window</button></body></html>")
		return
	}

	var stateData struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RedirectURI  string `json:"redirect_uri"`
		Provider     string `json:"provider"`
	}
	if stateParam := r.URL.Query().Get("state"); stateParam != "" {
		if raw, err := base64.RawURLEncoding.DecodeString(stateParam); err == nil {
			_ = json.Unmarshal(raw, &stateData)
		}
	}

	redirectURI := stateData.RedirectURI
	if redirectURI == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		callbackProvider := provider
		if callbackProvider == "gdrive" {
			callbackProvider = "google"
		}
		redirectURI = fmt.Sprintf("%s://%s/api/auth/%s/callback", scheme, r.Host, callbackProvider)
	}

	accID := fmt.Sprintf("%s_%d", provider, time.Now().Unix())
	accName := fmt.Sprintf("%s Account", strings.ToUpper(provider))
	quotaTotal := int64(15 * 1024 * 1024 * 1024)
	quotaUsed := int64(0)

	// Attempt real token exchange with provider
	tokenResp, tokenErr := auth.ExchangeCode(r.Context(), provider, code, redirectURI, stateData.ClientID, stateData.ClientSecret)
	if tokenErr != nil || tokenResp == nil || tokenResp.AccessToken == "" {
		errMsg := "Gagal mendapatkan token OAuth dari Google."
		if tokenErr != nil {
			errMsg = tokenErr.Error()
		}
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Authentication Failed</title>
<style>body { font-family: -apple-system, sans-serif; background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; text-align: center; }
.card { background: #1e293b; padding: 36px; border-radius: 16px; border: 1px solid #ef4444; max-width: 520px; box-shadow: 0 10px 25px rgba(0,0,0,0.5); }
h2 { color: #ef4444; margin-bottom: 12px; }
p { color: #cbd5e1; line-height: 1.5; font-size: 0.95rem; }
.err { background: rgba(239,68,68,0.15); color: #fca5a5; padding: 12px; border-radius: 8px; font-family: monospace; font-size: 0.85rem; margin: 16px 0; word-break: break-all; }
button { background: #334155; color: #fff; border: 0; padding: 10px 24px; border-radius: 8px; cursor: pointer; font-size: 0.9rem; }
button:hover { background: #475569; }
</style>
</head>
<body>
<div class="card">
  <h2>Autentikasi Gagal</h2>
  <p>Cloudgate tidak dapat menyelesaikan token exchange dengan Google OAuth.</p>
  <div class="err">%s</div>
  <p style="font-size:0.85rem;color:#94a3b8;margin-bottom:20px;">Pastikan <strong>Google OAuth Client ID</strong> dan <strong>Client Secret</strong> dimasukkan dengan benar pada form Add Account.</p>
  <button onclick="window.close()">Tutup Jendela</button>
</div>
</body>
</html>`, html.EscapeString(errMsg))
		return
	}

	// Fetch provider-specific user info and instantiate live driver
	var userEmail, userName string
	var liveDriver storage.Driver
	switch provider {
	case "google", "gdrive":
		userReq, err := http.NewRequestWithContext(r.Context(), "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
		if err == nil {
			userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
			userResp, err := http.DefaultClient.Do(userReq)
			if err == nil {
				defer userResp.Body.Close()
				var userInfo struct {
					Email string `json:"email"`
					Name  string `json:"name"`
				}
				if err := json.NewDecoder(userResp.Body).Decode(&userInfo); err == nil {
					userEmail = userInfo.Email
					userName = userInfo.Name
					if userInfo.Name != "" && userInfo.Email != "" {
						accName = fmt.Sprintf("%s (%s)", userInfo.Name, userInfo.Email)
					} else if userInfo.Email != "" {
						accName = userInfo.Email
					}
				}
			}
		}
		gdriver := storage.NewGDriveDriver(accID, stateData.ClientID, stateData.ClientSecret, tokenResp.AccessToken, tokenResp.RefreshToken, userEmail, userName)
		liveDriver = gdriver
	case "onedrive":
		userReq, err := http.NewRequestWithContext(r.Context(), "GET", "https://graph.microsoft.com/v1.0/me", nil)
		if err == nil {
			userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
			userResp, err := http.DefaultClient.Do(userReq)
			if err == nil {
				defer userResp.Body.Close()
				var userInfo struct {
					DisplayName string `json:"displayName"`
					Mail        string `json:"mail"`
					UserPrincipal string `json:"userPrincipalName"`
				}
				if err := json.NewDecoder(userResp.Body).Decode(&userInfo); err == nil {
					userEmail = userInfo.Mail
					if userEmail == "" {
						userEmail = userInfo.UserPrincipal
					}
					userName = userInfo.DisplayName
					if userInfo.DisplayName != "" && userEmail != "" {
						accName = fmt.Sprintf("%s (%s)", userInfo.DisplayName, userEmail)
					} else if userEmail != "" {
						accName = userEmail
					} else if userInfo.DisplayName != "" {
						accName = userInfo.DisplayName
					}
				}
			}
		}
		adapter := storage.NewRcloneAdapter("onedrive", accID, stateData.ClientID, stateData.ClientSecret, tokenResp.AccessToken, tokenResp.RefreshToken, userEmail, userName)
		liveDriver = adapter
	case "dropbox":
		userReq, err := http.NewRequestWithContext(r.Context(), "POST", "https://api.dropboxapi.com/2/users/get_current_account", nil)
		if err == nil {
			userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
			userResp, err := http.DefaultClient.Do(userReq)
			if err == nil {
				defer userResp.Body.Close()
				var userInfo struct {
					Name struct {
						DisplayName string `json:"display_name"`
					} `json:"name"`
					Email string `json:"email"`
				}
				if err := json.NewDecoder(userResp.Body).Decode(&userInfo); err == nil {
					userEmail = userInfo.Email
					userName = userInfo.Name.DisplayName
					if userInfo.Name.DisplayName != "" && userInfo.Email != "" {
						accName = fmt.Sprintf("%s (%s)", userInfo.Name.DisplayName, userInfo.Email)
					} else if userInfo.Email != "" {
						accName = userInfo.Email
					} else if userInfo.Name.DisplayName != "" {
						accName = userInfo.Name.DisplayName
					}
				}
			}
		}
		adapter := storage.NewRcloneAdapter("dropbox", accID, stateData.ClientID, stateData.ClientSecret, tokenResp.AccessToken, tokenResp.RefreshToken, userEmail, userName)
		liveDriver = adapter
	default:
		// Fallback: treat as generic dropbox-style rclone adapter if provider is known to auth
		adapter := storage.NewRcloneAdapter(provider, accID, stateData.ClientID, stateData.ClientSecret, tokenResp.AccessToken, tokenResp.RefreshToken, "", "")
		liveDriver = adapter
	}

	quota, qErr := liveDriver.About(r.Context())
	if qErr == nil {
		quotaTotal = quota.Total
		quotaUsed = quota.Used
	}

	s.mu.Lock()
	s.drivers[accID] = liveDriver
	if allPool, ok := s.pools["all_pool"]; ok {
		allPool.AddDriver(liveDriver)
	}
	s.mu.Unlock()

	// Trigger background indexing of real files
	go func(drv storage.Driver) {
		files, err := drv.List(context.Background(), "/")
		if err == nil {
			var indexed []db.IndexedFile
			for _, f := range files {
				indexed = append(indexed, db.IndexedFile{
					AccountID: accID,
					Path:      f.Path,
					Name:      f.Name,
					Size:      f.Size,
					IsDir:     f.IsDir,
					ModTime:   f.ModTime,
				})
			}
			_ = s.database.IndexFiles(indexed)
		}
	}(liveDriver)

	credsBytes, _ := json.Marshal(map[string]string{
		"client_id":     stateData.ClientID,
		"client_secret": stateData.ClientSecret,
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
	})

	acc := db.RemoteAccount{
		ID:          accID,
		Provider:    provider,
		Name:        accName,
		RootFolder:  "/",
		Status:      "connected",
		QuotaTotal:  quotaTotal,
		QuotaUsed:   quotaUsed,
		Credentials: string(credsBytes),
		UpdatedAt:   time.Now().UTC(),
	}

	_ = s.database.SaveAccount(acc)
	_ = s.database.RecordAudit("connect", acc.Name, acc.ID, "Connected via OAuth", "success", 0)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Authentication Successful</title>
<style>
body { font-family: -apple-system, sans-serif; background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; text-align: center; }
.card { background: #1e293b; padding: 40px; border-radius: 16px; border: 1px solid #334155; }
h2 { color: #10b981; margin-bottom: 12px; }
</style>
</head>
<body>
<div class="card">
  <h2>✓ Authenticated with %s</h2>
  <p style="color:#94a3b8;">Account <strong>%s</strong> has been connected successfully to Cloudgate.</p>
  <p style="font-size:0.85rem;color:#64748b;">This window will close automatically...</p>
</div>
<script>
if (window.opener) {
  window.opener.postMessage({ type: 'oauth_complete', provider: '%s', account_id: '%s' }, '*');
}
setTimeout(() => window.close(), 1500);
</script>
</body>
</html>`, strings.ToUpper(provider), accName, provider, acc.ID)

	fmt.Fprint(w, html)
}

func (s *Server) handleGetPools(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type PoolItem struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var list []PoolItem
	for id, p := range s.pools {
		list = append(list, PoolItem{ID: id, Name: p.Name()})
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	poolID := r.URL.Query().Get("pool_id")
	dirPath := r.URL.Query().Get("path")

	s.mu.RLock()
	defer s.mu.RUnlock()

	if poolID != "" {
		pool, ok := s.pools[poolID]
		if !ok {
			writeError(w, http.StatusNotFound, "pool not found")
			return
		}
		files, err := pool.UnifiedList(r.Context(), dirPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, files)
		return
	}

	if accountID != "" {
		driver, ok := s.drivers[accountID]
		if !ok {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		files, err := driver.List(r.Context(), dirPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, files)
		return
	}

	writeError(w, http.StatusBadRequest, "must provide either account_id or pool_id query parameter")
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	accountID := r.URL.Query().Get("account_id")
	poolID := r.URL.Query().Get("pool_id")
	targetPath := r.URL.Query().Get("path")

	if targetPath == "" {
		targetPath = "/"
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse multipart file")
		return
	}
	defer file.Close()

	destFilePath := path.Join(targetPath, header.Filename)

	s.mu.RLock()
	defer s.mu.RUnlock()

	var placedAccount string

	if poolID != "" {
		pool, ok := s.pools[poolID]
		if !ok {
			writeError(w, http.StatusNotFound, "pool not found")
			return
		}
		accID, err := pool.Write(r.Context(), destFilePath, file, header.Size)
		if err != nil {
			_ = s.database.RecordAudit("upload", destFilePath, poolID, err.Error(), "failed", time.Since(start).Milliseconds())
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		placedAccount = accID
	} else if accountID != "" {
		driver, ok := s.drivers[accountID]
		if !ok {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		if err := driver.Put(r.Context(), destFilePath, file, header.Size); err != nil {
			_ = s.database.RecordAudit("upload", destFilePath, accountID, err.Error(), "failed", time.Since(start).Milliseconds())
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		placedAccount = accountID
	} else {
		writeError(w, http.StatusBadRequest, "must provide either account_id or pool_id")
		return
	}

	_ = s.database.RecordAudit("upload", destFilePath, placedAccount, fmt.Sprintf("%d bytes", header.Size), "success", time.Since(start).Milliseconds())
	writeJSON(w, http.StatusCreated, map[string]any{
		"path":       destFilePath,
		"account_id": placedAccount,
		"size":       header.Size,
	})
}

func detectContentType(filePath string) string {
	ext := strings.ToLower(path.Ext(filePath))
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".txt", ".log", ".ini", ".conf", ".env":
		return "text/plain; charset=utf-8"
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".go", ".py", ".rs", ".java", ".c", ".cpp", ".h", ".sh", ".bash", ".sql", ".yaml", ".yml", ".xml", ".ts", ".tsx", ".jsx":
		return "text/plain; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogv":
		return "video/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	filePath := r.URL.Query().Get("path")
	inline := r.URL.Query().Get("inline") == "true" || r.URL.Query().Get("preview") == "true"

	s.mu.RLock()
	driver, ok := s.drivers[accountID]
	if !ok && accountID == "" {
		for id, drv := range s.drivers {
			if _, _, err := drv.Get(r.Context(), filePath); err == nil {
				accountID = id
				driver = drv
				ok = true
				break
			}
		}
	}
	s.mu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	rc, info, err := driver.Get(r.Context(), filePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	defer rc.Close()

	fileName := path.Base(filePath)
	cType := detectContentType(filePath)

	if inline {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, fileName))
		w.Header().Set("Content-Type", cType)
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
		w.Header().Set("Content-Type", cType)
	}
	if info.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}

	_, _ = io.Copy(w, rc)
	_ = s.database.RecordAudit("download", filePath, accountID, "", "success", 0)
}

func (s *Server) handleCopyFile(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req struct {
		SrcAccountID string `json:"src_account_id"`
		SrcPath      string `json:"src_path"`
		DstAccountID string `json:"dst_account_id"`
		DstPath      string `json:"dst_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	s.mu.RLock()
	srcDriver, ok1 := s.drivers[req.SrcAccountID]
	dstDriver, ok2 := s.drivers[req.DstAccountID]
	s.mu.RUnlock()

	if !ok1 || !ok2 {
		writeError(w, http.StatusNotFound, "source or destination account not found")
		return
	}

	if err := storage.CopyFile(r.Context(), srcDriver, req.SrcPath, dstDriver, req.DstPath); err != nil {
		_ = s.database.RecordAudit("copy", req.SrcPath, req.SrcAccountID, err.Error(), "failed", time.Since(start).Milliseconds())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.database.RecordAudit("copy", req.SrcPath+" -> "+req.DstPath, req.DstAccountID, "success", "success", time.Since(start).Milliseconds())
	writeJSON(w, http.StatusOK, map[string]string{"status": "copied"})
}

func (s *Server) handleMoveFile(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req struct {
		SrcAccountID string `json:"src_account_id"`
		SrcPath      string `json:"src_path"`
		DstAccountID string `json:"dst_account_id"`
		DstPath      string `json:"dst_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	s.mu.RLock()
	srcDriver, ok1 := s.drivers[req.SrcAccountID]
	dstDriver, ok2 := s.drivers[req.DstAccountID]
	s.mu.RUnlock()

	if !ok1 || !ok2 {
		writeError(w, http.StatusNotFound, "source or destination account not found")
		return
	}

	if err := storage.MoveFile(r.Context(), srcDriver, req.SrcPath, dstDriver, req.DstPath); err != nil {
		_ = s.database.RecordAudit("move", req.SrcPath, req.SrcAccountID, err.Error(), "failed", time.Since(start).Milliseconds())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.database.RecordAudit("move", req.SrcPath+" -> "+req.DstPath, req.DstAccountID, "success", "success", time.Since(start).Milliseconds())
	writeJSON(w, http.StatusOK, map[string]string{"status": "moved"})
}

func (s *Server) handleTrashFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"account_id"`
		Path      string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	s.mu.RLock()
	driver, ok := s.drivers[req.AccountID]
	s.mu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	rec, err := s.trashManager.MoveToTrash(r.Context(), driver, req.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.database.RecordAudit("trash", req.Path, req.AccountID, "Moved to trash", "success", 0)
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleGetTrash(w http.ResponseWriter, r *http.Request) {
	records, err := s.database.GetTrashRecords()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) handleRestoreTrash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TrashID string `json:"trash_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	records, err := s.database.GetTrashRecords()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var accountID, originalPath string
	for _, rec := range records {
		if rec.ID == req.TrashID {
			accountID = rec.AccountID
			originalPath = rec.OriginalPath
			break
		}
	}

	s.mu.RLock()
	driver, ok := s.drivers[accountID]
	s.mu.RUnlock()

	if !ok {
		writeError(w, http.StatusNotFound, "account for trash record not found")
		return
	}

	if err := s.trashManager.Restore(r.Context(), driver, req.TrashID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.database.RecordAudit("restore", originalPath, accountID, "Restored from trash", "success", 0)
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

func (s *Server) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	drivers := make([]storage.Driver, 0, len(s.drivers))
	for _, d := range s.drivers {
		drivers = append(drivers, d)
	}
	s.mu.RUnlock()

	for _, d := range drivers {
		_ = s.trashManager.EmptyTrash(r.Context(), d)
	}

	_ = s.database.RecordAudit("empty_trash", "all", "all", "Trash emptied", "success", 0)
	writeJSON(w, http.StatusOK, map[string]string{"status": "emptied"})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := s.database.SearchFiles(query, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleGetAudit(w http.ResponseWriter, r *http.Request) {
	events, err := s.database.GetRecentAuditEvents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleExportAudit(w http.ResponseWriter, r *http.Request) {
	events, err := s.database.GetRecentAuditEvents()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\"cloudgate-audit-history.json\"")
	_ = json.NewEncoder(w).Encode(events)
}

func (s *Server) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	res, err := updater.CheckUpdate(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSyncPush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token      string `json:"token"`
		Repo       string `json:"repo"`
		Path       string `json:"path"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	client, err := ghsync.NewGitHubSyncClient(req.Token, req.Repo, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	accounts, _ := s.database.GetAccounts()
	payload, _ := json.Marshal(accounts)

	if err := client.PushVault(r.Context(), payload, req.Passphrase); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.database.RecordAudit("sync_push", req.Repo, "github", "Encrypted vault backed up", "success", 0)
	writeJSON(w, http.StatusOK, map[string]string{"status": "synced"})
}

func (s *Server) handleSyncPull(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token      string `json:"token"`
		Repo       string `json:"repo"`
		Path       string `json:"path"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	client, err := ghsync.NewGitHubSyncClient(req.Token, req.Repo, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	decrypted, err := client.PullVault(r.Context(), req.Passphrase)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var accounts []db.RemoteAccount
	if err := json.Unmarshal(decrypted, &accounts); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse decrypted accounts")
		return
	}

	for _, acc := range accounts {
		_ = s.database.SaveAccount(acc)
	}

	_ = s.database.RecordAudit("sync_pull", req.Repo, "github", "Encrypted vault restored", "success", 0)
	writeJSON(w, http.StatusOK, accounts)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
