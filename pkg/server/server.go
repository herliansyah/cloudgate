package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"sort"
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
	mu             sync.RWMutex
	database       *db.DB
	trashManager   *storage.TrashManager
	taskManager    *storage.TaskManager
	drivers        map[string]storage.Driver
	pools          map[string]*storage.StoragePool
	assets         fs.FS
	restartTrigger func()

	driverFactory DriverFactory

	oauthMu     sync.Mutex
	oauthStates map[string]pendingOAuth
}

func NewServer(database *db.DB, assets fs.FS) *Server {
	s := &Server{
		database:      database,
		trashManager:  storage.NewTrashManager(database),
		drivers:       make(map[string]storage.Driver),
		pools:         make(map[string]*storage.StoragePool),
		assets:        assets,
		driverFactory: defaultDriverFactory,
		oauthStates:   make(map[string]pendingOAuth),
	}
	s.taskManager = storage.NewTaskManager(database, s.trashManager, func(accountID string) (storage.Driver, error) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		drv, ok := s.drivers[accountID]
		if !ok || drv == nil {
			return nil, fmt.Errorf("account %s not found or disconnected", accountID)
		}
		return drv, nil
	})
	_ = s.taskManager.Start()
	_ = database.EnableFTSIndex()

	// Start background updater check asynchronously
	go func() {
		time.Sleep(3 * time.Second)
		_, _ = updater.CheckUpdate(context.Background(), false)
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			_, _ = updater.CheckUpdate(context.Background(), true)
		}
	}()

	return s
}

// SetRestartTrigger configures the callback executed when an update requires a process restart.
func (s *Server) SetRestartTrigger(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restartTrigger = fn
}

func (s *Server) triggerRestart() {
	s.mu.RLock()
	fn := s.restartTrigger
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// Close gracefully terminates background tasks and releases resources.
func (s *Server) Close() {
	if s.taskManager != nil {
		s.taskManager.Stop()
	}
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
	mux.HandleFunc("GET /api/providers", s.handleProviders)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/accounts", s.handleGetAccounts)
	mux.HandleFunc("POST /api/accounts", s.handleAddAccount)
	mux.HandleFunc("POST /api/accounts/test", s.handleTestConnectionNew)
	mux.HandleFunc("POST /api/accounts/{id}/toggle", s.handleToggleAccount)
	mux.HandleFunc("POST /api/accounts/{id}/test", s.handleTestAccount)
	mux.HandleFunc("PATCH /api/accounts/{id}", s.handleUpdateAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", s.handleDeleteAccount)
	mux.HandleFunc("POST /api/storage/sync", s.handleSyncStorage)

	// GatewayAuth Routes
	mux.HandleFunc("GET /api/auth/gateway/status", s.handleGatewayStatus)
	mux.HandleFunc("POST /api/auth/gateway/setup", s.handleGatewaySetup)
	mux.HandleFunc("POST /api/auth/gateway/unlock", s.handleGatewayUnlock)
	mux.HandleFunc("POST /api/auth/gateway/lock", s.handleGatewayLock)
	mux.HandleFunc("POST /api/auth/gateway/change-password", s.handleGatewayChangePassword)

	// OAuth Routes
	mux.HandleFunc("GET /api/auth/{provider}/login", s.handleOAuthLogin)
	mux.HandleFunc("POST /api/auth/{provider}/login", s.handleOAuthLogin)
	mux.HandleFunc("GET /api/auth/{provider}/callback", s.handleOAuthCallback)

	mux.HandleFunc("GET /api/pools", s.handleGetPools)

	mux.HandleFunc("GET /api/files", s.handleListFiles)
	mux.HandleFunc("GET /api/files/starred", s.handleGetStarred)
	mux.HandleFunc("POST /api/files/starred", s.handleAddStarred)
	mux.HandleFunc("DELETE /api/files/starred", s.handleRemoveStarred)
	mux.HandleFunc("GET /api/files/recent", s.handleGetRecentFiles)
	mux.HandleFunc("GET /api/files/share", s.handleGetShareLink)
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
	mux.HandleFunc("POST /api/updater/apply", s.handleApplyUpdate)
	mux.HandleFunc("GET /api/changelog", s.handleGetChangelog)
	mux.HandleFunc("POST /api/sync/github/push", s.handleSyncPush)
	mux.HandleFunc("POST /api/sync/github/pull", s.handleSyncPull)

	// Background Task Routes
	mux.HandleFunc("GET /api/tasks", s.handleListTasks)
	mux.HandleFunc("POST /api/tasks/transfer", s.handleCreateTransferTask)
	mux.HandleFunc("POST /api/tasks/ingest", s.handleCreateIngestTask)
	mux.HandleFunc("POST /api/tasks/replicate", s.handleCreateReplicateTask)
	mux.HandleFunc("POST /api/tasks/{id}/cancel", s.handleCancelTask)
	mux.HandleFunc("POST /api/tasks/{id}/retry", s.handleRetryTask)
	mux.HandleFunc("DELETE /api/tasks/finished", s.handleClearFinishedTasks)

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

	return s.corsMiddleware(s.authMiddleware(mux))
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

func isLoopbackRequest(r *http.Request) bool {
	if r.RemoteAddr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasPassword, err := s.database.HasMasterPassword()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		path := r.URL.Path

		// Non-API paths (static assets, HTML, CSS, JS) are served freely so UI can render lock/setup screen
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// When MasterPassword is not yet configured (Mandatory First-Run Setup state):
		if !hasPassword {
			if path == "/api/auth/gateway/status" || path == "/api/info" || path == "/api/providers" || path == "/api/changelog" {
				next.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(path, "/api/auth/") && strings.HasSuffix(path, "/callback") {
				next.ServeHTTP(w, r)
				return
			}
			if path == "/api/auth/gateway/setup" {
				if isLoopbackRequest(r) {
					next.ServeHTTP(w, r)
					return
				}
				writeError(w, http.StatusForbidden, "Inisialisasi MasterPassword hanya diizinkan dari localhost.")
				return
			}
			writeError(w, http.StatusUnauthorized, "Cloudgate belum diinisialisasi. Silakan buat MasterPassword terlebih dahulu dari localhost atau CLI.")
			return
		}

		// Whitelisted paths when MasterPassword is configured:
		if path == "/api/auth/gateway/status" ||
			path == "/api/auth/gateway/unlock" ||
			path == "/api/auth/gateway/setup" ||
			path == "/api/info" ||
			path == "/api/providers" ||
			path == "/api/changelog" ||
			(strings.HasPrefix(path, "/api/auth/") && strings.HasSuffix(path, "/callback")) {
			next.ServeHTTP(w, r)
			return
		}

		// Check session cookie or Authorization Bearer header
		token := ""
		if cookie, err := r.Cookie("cg_session"); err == nil && cookie.Value != "" {
			token = cookie.Value
		} else if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if token != "" {
			valid, err := s.database.ValidateGatewaySession(token)
			if err == nil && valid {
				next.ServeHTTP(w, r)
				return
			}
		}

		writeError(w, http.StatusUnauthorized, "GatewayAuth terkunci. Silakan masukkan MasterPassword.")
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"app":          config.AppName,
		"version":      config.AppVersion,
		"author":       config.AppAuthor,
		"repository":   config.AppRepo,
		"donation_url": config.AppDonationURL,
		"sponsor_url":  config.AppDonationURL,
	})
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	// Static provider registry: all 16 providers are served by embedded rclone backends (ADR-0027).
	providers := []map[string]any{
		{"id": "gdrive", "name": "Google Drive", "category": "Cloud Drive", "auth_method": "oauth", "description": "Google Drive cloud storage via OAuth 2.0"},
		{"id": "onedrive", "name": "OneDrive", "category": "Cloud Drive", "auth_method": "oauth", "description": "Microsoft OneDrive via OAuth 2.0"},
		{"id": "dropbox", "name": "Dropbox", "category": "Cloud Drive", "auth_method": "oauth", "description": "Dropbox cloud storage via OAuth 2.0"},
		{"id": "box", "name": "Box", "category": "Cloud Drive", "auth_method": "oauth", "description": "Box cloud storage via OAuth 2.0"},
		{"id": "pcloud", "name": "pCloud", "category": "Cloud Drive", "auth_method": "oauth", "description": "pCloud storage via OAuth 2.0"},
		{"id": "yandex", "name": "Yandex Disk", "category": "Cloud Drive", "auth_method": "oauth", "description": "Yandex Disk storage via OAuth 2.0"},
		{"id": "koofr", "name": "Koofr", "category": "Cloud Drive", "auth_method": "credentials", "description": "Koofr storage via email and app password"},
		{"id": "mega", "name": "MEGA", "category": "Privacy Cloud", "auth_method": "credentials", "description": "Client-side encrypted MEGA storage"},
		{"id": "filen", "name": "Filen", "category": "Privacy Cloud", "auth_method": "credentials", "description": "Zero-knowledge end-to-end encrypted Filen cloud"},
		{"id": "b2", "name": "Backblaze B2", "category": "Object Storage", "auth_method": "access_keys", "description": "Backblaze B2 Cloud Object Storage"},
		{"id": "pikpak", "name": "PikPak", "category": "Privacy Cloud", "auth_method": "credentials", "description": "PikPak private cloud drive"},
		{"id": "sftp", "name": "SFTP / SSH", "category": "Server Protocol", "auth_method": "ssh", "description": "Secure File Transfer Protocol over SSH"},
		{"id": "smb", "name": "SMB / Samba", "category": "Server Protocol", "auth_method": "network_share", "description": "Server Message Block (Windows Share / Samba)"},
		{"id": "protondrive", "name": "Proton Drive", "category": "Privacy Cloud", "auth_method": "credentials", "description": "Zero-knowledge Proton Drive storage"},
		{"id": "s3", "name": "Amazon S3", "category": "Object Storage", "auth_method": "access_keys", "description": "AWS S3 and S3-compatible object storage"},
		{"id": "webdav", "name": "WebDAV", "category": "Protocol / Cloud", "auth_method": "credentials", "description": "Nextcloud, ownCloud, and standard WebDAV"},
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.database.GetAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, redactAccounts(accounts))
}

func providerDisplayName(provider string) string {
	return storage.ProviderLabel(storage.NormalizeProvider(provider))
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing account id")
		return
	}

	acc, err := s.database.GetAccount(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	var req struct {
		Name       *string `json:"name"`
		RootFolder *string `json:"root_folder"`
		QuotaTotal *int64  `json:"quota_total"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	rootFolderChanged := false
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		acc.Name = trimmed
	}

	if req.RootFolder != nil {
		rf := strings.TrimSpace(*req.RootFolder)
		if rf == "" {
			rf = "/"
		}
		if rf != acc.RootFolder {
			rootFolderChanged = true
			acc.RootFolder = rf
		}
	}

	if req.QuotaTotal != nil {
		if *req.QuotaTotal < 0 {
			writeError(w, http.StatusBadRequest, "quota_total cannot be negative")
			return
		}
		acc.QuotaTotal = *req.QuotaTotal
	}

	acc.UpdatedAt = time.Now().UTC()
	if err := s.database.SaveAccount(*acc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if rootFolderChanged {
		_ = s.database.ClearAccountIndex(id)
	}

	_ = s.database.RecordAudit("update", acc.Name, acc.ID, fmt.Sprintf("Updated RemoteAccount: name='%s', root_folder='%s'", acc.Name, acc.RootFolder), "success", 0)
	writeJSON(w, http.StatusOK, redactAccount(*acc))
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

func (s *Server) handleToggleAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing account id")
		return
	}
	acc, err := s.database.GetAccount(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	newEnabled := !acc.Enabled
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Enabled != nil {
		newEnabled = *req.Enabled
	}

	if err := s.database.ToggleAccount(id, newEnabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.mu.Lock()
	if allPool, ok := s.pools["all_pool"]; ok {
		if newEnabled {
			if drv, exists := s.drivers[id]; exists {
				allPool.AddDriver(drv)
			}
		} else {
			allPool.RemoveDriver(id)
		}
	}
	s.mu.Unlock()

	action := "disable"
	if newEnabled {
		action = "enable"
	}
	_ = s.database.RecordAudit(action, acc.Name, acc.ID, fmt.Sprintf("Account integration %sd", action), "success", 0)

	acc.Enabled = newEnabled
	if newEnabled {
		acc.Status = "connected"
	} else {
		acc.Status = "disabled"
	}
	writeJSON(w, http.StatusOK, redactAccount(*acc))
}

func (s *Server) handleTestAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing account id")
		return
	}
	s.mu.RLock()
	drv, ok := s.drivers[id]
	s.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "active driver not found for account")
		return
	}

	start := time.Now()
	err := drv.TestConnection(r.Context())
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		_ = s.database.RecordAudit("test_connection", id, id, err.Error(), "failed", durationMs)
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     false,
			"error":       err.Error(),
			"duration_ms": durationMs,
		})
		return
	}

	quota, _ := drv.About(r.Context())
	_ = s.database.RecordAudit("test_connection", id, id, "Handshake verified", "success", durationMs)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"duration_ms": durationMs,
		"quota":       quota,
	})
}

func (s *Server) handleSyncStorage(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	drivers := make(map[string]storage.Driver, len(s.drivers))
	for k, v := range s.drivers {
		drivers[k] = v
	}
	s.mu.RUnlock()

	now := time.Now().UTC()
	updatedCount := 0
	for id, drv := range drivers {
		syncCtx, syncCancel := context.WithTimeout(r.Context(), 3*time.Second)
		quota, err := drv.About(syncCtx)
		syncCancel()
		acc, accErr := s.database.GetAccount(id)
		if accErr != nil || acc == nil {
			continue
		}
		if err == nil {
			acc.QuotaTotal = quota.Total
			acc.QuotaUsed = quota.Used
		}
		acc.LastSyncAt = &now

		// Auto-reconcile AccountPrincipal if empty
		if acc.Email == "" {
			if ue, ok := drv.(interface{ UserEmail() string }); ok {
				if email := ue.UserEmail(); email != "" {
					acc.Email = email
				}
			}
		}
		// Upgrade generic fallback names
		if acc.Email != "" && (strings.HasSuffix(acc.Name, "Account") || strings.EqualFold(acc.Name, acc.Provider) || strings.EqualFold(acc.Name, providerDisplayName(acc.Provider))) {
			acc.Name = fmt.Sprintf("%s (%s)", providerDisplayName(acc.Provider), acc.Email)
		}

		_ = s.database.SaveAccount(*acc)
		_ = s.database.UpdateAccountLastSync(id, now)
		updatedCount++
	}

	_ = s.database.RecordAudit("sync", "StorageHub", "system", fmt.Sprintf("Reconciled %d accounts", updatedCount), "success", 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "synced",
		"synced_at":      now,
		"accounts_count": updatedCount,
	})
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

	if poolID == "" && accountID == "" {
		poolID = "all_pool"
	}

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

func (s *Server) handleGetStarred(w http.ResponseWriter, r *http.Request) {
	list, err := s.database.GetStarredFiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAddStarred(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"account_id"`
		Path      string `json:"path"`
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		IsDir     bool   `json:"is_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AccountID == "" || req.Path == "" {
		writeError(w, http.StatusBadRequest, "account_id and path required")
		return
	}
	if req.Name == "" {
		req.Name = path.Base(req.Path)
	}
	rec := db.StarredRecord{
		ID:        fmt.Sprintf("%s:%s", req.AccountID, req.Path),
		AccountID: req.AccountID,
		Path:      req.Path,
		Name:      req.Name,
		Size:      req.Size,
		IsDir:     req.IsDir,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.database.AddStarredFile(rec); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) handleRemoveStarred(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	filePath := r.URL.Query().Get("path")
	id := r.URL.Query().Get("id")

	if id != "" {
		_ = s.database.RemoveStarredFile(id, "")
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
		return
	}
	if accountID != "" && filePath != "" {
		_ = s.database.RemoveStarredFile(accountID, filePath)
		writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
		return
	}
	writeError(w, http.StatusBadRequest, "must provide id or account_id and path")
}

func (s *Server) handleGetRecentFiles(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	allPool, ok := s.pools["all_pool"]
	s.mu.RUnlock()
	if ok {
		files, err := allPool.UnifiedList(r.Context(), "/")
		if err == nil && len(files) > 0 {
			sort.Slice(files, func(i, j int) bool {
				return files[i].ModTime.After(files[j].ModTime)
			})
			limit := 30
			if len(files) > limit {
				files = files[:limit]
			}
			writeJSON(w, http.StatusOK, files)
			return
		}
	}
	writeJSON(w, http.StatusOK, []storage.FileInfo{})
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
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	switch ext {
	case ".md", ".markdown", ".go", ".py", ".rs", ".java", ".c", ".cpp", ".h", ".sh", ".bash", ".sql", ".yaml", ".yml", ".env", ".log", ".conf", ".ini", ".ts", ".tsx", ".jsx":
		return "text/plain; charset=utf-8"
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
			if _, err := storage.Stat(r.Context(), drv, filePath); err == nil {
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
	force := r.URL.Query().Get("force") == "true"
	res, err := updater.CheckUpdate(r.Context(), force)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DownloadURL string `json:"download_url"`
		ChecksumURL string `json:"checksum_url"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	downloadURL := req.DownloadURL
	checksumURL := req.ChecksumURL

	// If URLs not explicitly provided in body, discover automatically via CheckUpdate
	if downloadURL == "" || checksumURL == "" {
		res, err := updater.CheckUpdate(r.Context(), false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query update assets: %v", err))
			return
		}
		if !res.UpdateAvailable {
			writeError(w, http.StatusBadRequest, "No newer version available to apply")
			return
		}
		if downloadURL == "" {
			downloadURL = res.DownloadURL
		}
		if checksumURL == "" {
			checksumURL = res.ChecksumURL
		}
	}

	if err := updater.ApplyUpdate(r.Context(), downloadURL, checksumURL); err != nil {
		_ = s.database.RecordAudit("update", "system", "system", fmt.Sprintf("Update failed: %v", err), "failed", 0)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to apply update: %v", err))
		return
	}

	_ = s.database.RecordAudit("update", "system", "system", "ReleasePackage successfully verified and applied; restarting server", "success", 0)

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Pembaruan berhasil diterapkan. Server sedang memulai ulang...",
	})

	// Trigger graceful restart asynchronously after HTTP response is flushed
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.triggerRestart()
	}()
}

func (s *Server) handleGetChangelog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"changelog": updater.GetChangelog(),
	})
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
	writeJSON(w, http.StatusOK, redactAccounts(accounts))
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) handleGatewayStatus(w http.ResponseWriter, r *http.Request) {
	hasPassword, err := s.database.HasMasterPassword()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	loopback := isLoopbackRequest(r)
	authenticated := false
	if hasPassword {
		token := ""
		if cookie, err := r.Cookie("cg_session"); err == nil && cookie.Value != "" {
			token = cookie.Value
		} else if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
		if token != "" {
			valid, err := s.database.ValidateGatewaySession(token)
			if err == nil && valid {
				authenticated = true
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        hasPassword,
		"setup_required": !hasPassword,
		"can_setup":      !hasPassword && loopback,
		"authenticated":  authenticated,
	})
}

func (s *Server) handleGatewaySetup(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Inisialisasi MasterPassword hanya diizinkan dari localhost.")
		return
	}

	hasPassword, err := s.database.HasMasterPassword()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hasPassword {
		writeError(w, http.StatusBadRequest, "MasterPassword sudah disetel. Gunakan menu ganti kata sandi.")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if len(req.Password) < 4 {
		writeError(w, http.StatusBadRequest, "Kata sandi minimal 4 karakter")
		return
	}

	hash, err := auth.HashMasterPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.database.SetMasterPassword(hash); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	token, err := auth.GenerateSessionToken()
	if err == nil {
		_ = s.database.CreateGatewaySession(token, time.Now().Add(7*24*time.Hour))
		http.SetCookie(w, &http.Cookie{
			Name:     "cg_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   7 * 24 * 3600,
		})
	}

	_ = s.database.RecordAudit("auth", "gateway", "system", "MasterPassword diaktifkan", "success", 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   token,
		"message": "MasterPassword berhasil diaktifkan",
	})
}

func (s *Server) handleGatewayUnlock(w http.ResponseWriter, r *http.Request) {
	hash, err := s.database.GetMasterPasswordHash()
	if err != nil || hash == "" {
		writeError(w, http.StatusBadRequest, "GatewayAuth belum diaktifkan")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !auth.VerifyMasterPassword(hash, req.Password) {
		_ = s.database.RecordAudit("auth", "gateway", "system", "Percobaan unlock gateway gagal: kata sandi salah", "failed", 0)
		writeError(w, http.StatusUnauthorized, "Kata sandi salah")
		return
	}

	token, err := auth.GenerateSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Gagal membuat token sesi")
		return
	}

	if err := s.database.CreateGatewaySession(token, time.Now().Add(7*24*time.Hour)); err != nil {
		writeError(w, http.StatusInternalServerError, "Gagal menyimpan sesi")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "cg_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 3600,
	})

	_ = s.database.RecordAudit("auth", "gateway", "system", "GatewayAuth berhasil dibuka", "success", 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"token":   token,
		"message": "Gateway berhasil dibuka",
	})
}

func (s *Server) handleGatewayLock(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("cg_session"); err == nil && cookie.Value != "" {
		_ = s.database.DeleteGatewaySession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "cg_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	_ = s.database.RecordAudit("auth", "gateway", "system", "GatewayAuth dikunci", "success", 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Gateway berhasil dikunci",
	})
}

func (s *Server) handleGatewayChangePassword(w http.ResponseWriter, r *http.Request) {
	hash, err := s.database.GetMasterPasswordHash()
	if err != nil || hash == "" {
		writeError(w, http.StatusBadRequest, "GatewayAuth belum diaktifkan")
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !auth.VerifyMasterPassword(hash, req.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "Kata sandi saat ini salah")
		return
	}

	if len(req.NewPassword) < 4 {
		writeError(w, http.StatusBadRequest, "Kata sandi baru minimal 4 karakter")
		return
	}

	newHash, err := auth.HashMasterPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.database.SetMasterPassword(newHash); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.database.RecordAudit("auth", "gateway", "system", "MasterPassword berhasil diperbarui", "success", 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Kata sandi berhasil diperbarui",
	})
}

// Background Task Handlers

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	tasks, err := s.database.ListTasks(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tasks == nil {
		tasks = []db.StorageTask{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleCreateTransferTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceAccountID string `json:"source_account_id"`
		SourcePath      string `json:"source_path"`
		TargetAccountID string `json:"target_account_id"`
		TargetPath      string `json:"target_path"`
		IsDir           bool   `json:"is_dir"`
		IsMove          bool   `json:"is_move"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if req.SourceAccountID == "" || req.SourcePath == "" || req.TargetAccountID == "" || req.TargetPath == "" {
		writeError(w, http.StatusBadRequest, "source_account_id, source_path, target_account_id, and target_path are required")
		return
	}

	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	task := &db.StorageTask{
		ID:              taskID,
		Type:            "transfer",
		SourceAccountID: req.SourceAccountID,
		SourcePath:      req.SourcePath,
		TargetAccountID: req.TargetAccountID,
		TargetPath:      req.TargetPath,
		IsDir:           req.IsDir,
		IsMove:          req.IsMove,
		Status:          "pending",
	}

	if err := s.taskManager.Enqueue(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	action := "copy"
	if req.IsMove {
		action = "move"
	}
	_ = s.database.RecordAudit(action, req.SourcePath, req.SourceAccountID, fmt.Sprintf("Queued %s to %s on %s", action, req.TargetPath, req.TargetAccountID), "success", 0)

	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) handleCreateIngestTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL             string `json:"url"`
		TargetAccountID string `json:"target_account_id"`
		TargetPath      string `json:"target_path"`
		CustomName      string `json:"custom_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if req.URL == "" || req.TargetAccountID == "" {
		writeError(w, http.StatusBadRequest, "url and target_account_id are required")
		return
	}

	// Strict SSRF validation
	if _, err := storage.ValidateSSRF(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("SSRF check failed: %v", err))
		return
	}

	targetPath := req.TargetPath
	if targetPath == "" {
		targetPath = "/"
	}
	isDir := true
	if req.CustomName != "" {
		targetPath = path.Join(targetPath, req.CustomName)
		isDir = false
	}

	taskID := fmt.Sprintf("ingest_%d", time.Now().UnixNano())
	task := &db.StorageTask{
		ID:              taskID,
		Type:            "ingest",
		SourcePath:      req.URL,
		TargetAccountID: req.TargetAccountID,
		TargetPath:      targetPath,
		IsDir:           isDir,
		Status:          "pending",
	}

	if err := s.taskManager.Enqueue(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = s.database.RecordAudit("ingest", req.URL, req.TargetAccountID, fmt.Sprintf("Queued remote ingest to %s", targetPath), "success", 0)
	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) handleCreateReplicateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceAccountID string `json:"source_account_id"`
		SourcePath      string `json:"source_path"`
		TargetAccountID string `json:"target_account_id"`
		TargetPath      string `json:"target_path"`
		Mirror          bool   `json:"mirror"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if req.SourceAccountID == "" || req.SourcePath == "" || req.TargetAccountID == "" || req.TargetPath == "" {
		writeError(w, http.StatusBadRequest, "source_account_id, source_path, target_account_id, and target_path are required")
		return
	}

	// Validate same-account replicate constraints to avoid loops
	if req.SourceAccountID == req.TargetAccountID {
		srcClean := path.Clean("/" + req.SourcePath)
		dstClean := path.Clean("/" + req.TargetPath)
		if srcClean == dstClean {
			writeError(w, http.StatusBadRequest, "source and target folder cannot be identical on the same account")
			return
		}
		if srcClean == "/" || strings.HasPrefix(dstClean, srcClean+"/") {
			writeError(w, http.StatusBadRequest, "target folder cannot be inside the source folder on the same account")
			return
		}
	}

	taskID := fmt.Sprintf("rep_%d", time.Now().UnixNano())
	task := &db.StorageTask{
		ID:              taskID,
		Type:            "replicate",
		SourceAccountID: req.SourceAccountID,
		SourcePath:      req.SourcePath,
		TargetAccountID: req.TargetAccountID,
		TargetPath:      req.TargetPath,
		IsDir:           true,
		Mirror:          req.Mirror,
		Status:          "pending",
	}

	if err := s.taskManager.Enqueue(task); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	mode := "additive"
	if req.Mirror {
		mode = "mirror"
	}
	_ = s.database.RecordAudit("replicate", req.SourcePath, req.SourceAccountID, fmt.Sprintf("Queued folder replication (%s) to %s on %s", mode, req.TargetPath, req.TargetAccountID), "success", 0)

	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "task id required")
		return
	}

	if err := s.taskManager.Cancel(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id, "status": "cancelled"})
}

func (s *Server) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "task id required")
		return
	}

	task, err := s.taskManager.Retry(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleClearFinishedTasks(w http.ResponseWriter, r *http.Request) {
	if err := s.database.ClearFinishedTasks(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Riwayat task yang selesai telah dibersihkan"})
}
