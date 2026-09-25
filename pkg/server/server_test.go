package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/server"
	"github.com/herliansyah/cloudgate/pkg/storage"
)

func TestServerAndAPI(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloudgate_server_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := server.NewServer(database, nil)
	memDriver := storage.NewMemDriver("test_drive", "gdrive", 1024*1024)
	srv.RegisterDriver(memDriver)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	setupTestAuth(t, database, ts.URL)

	// 1. Test /api/info (Verifying Author: Herliansyah and Repo)
	resp, err := http.Get(ts.URL + "/api/info")
	if err != nil {
		t.Fatalf("failed to get /api/info: %v", err)
	}
	var info map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()

	if info["author"] != "Herliansyah" {
		t.Fatalf("expected author Herliansyah, got %s", info["author"])
	}
	if info["repository"] != "https://github.com/herliansyah/cloudgate" {
		t.Fatalf("expected repo https://github.com/herliansyah/cloudgate, got %s", info["repository"])
	}

	// 2. Test /api/stats
	resp, err = http.Get(ts.URL + "/api/stats")
	if err != nil {
		t.Fatalf("failed to get /api/stats: %v", err)
	}
	var stats map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&stats)
	resp.Body.Close()
	if stats["total_storage"].(float64) <= 0 {
		t.Fatalf("expected positive total storage")
	}

	// 3. Test /api/files/upload (Multipart upload)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "hello.txt")
	_, _ = part.Write([]byte("Hello Cloudgate World!"))
	_ = writer.Close()

	uploadReq, _ := http.NewRequest("POST", ts.URL+"/api/files/upload?account_id=test_drive&path=/", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil || uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("file upload failed, status: %v, err: %v", uploadResp.StatusCode, err)
	}
	uploadResp.Body.Close()

	// 4. Test /api/files/download
	downResp, err := http.Get(ts.URL + "/api/files/download?account_id=test_drive&path=/hello.txt")
	if err != nil || downResp.StatusCode != http.StatusOK {
		t.Fatalf("file download failed, status: %v", downResp.StatusCode)
	}
	content, _ := io.ReadAll(downResp.Body)
	downResp.Body.Close()
	if string(content) != "Hello Cloudgate World!" {
		t.Fatalf("unexpected content: %s", string(content))
	}

	// 5. Test FTS5 Search Indexing & Query
	_ = database.IndexFiles([]db.IndexedFile{
		{
			AccountID: "test_drive",
			Path:      "/hello.txt",
			Name:      "hello.txt",
			Size:      int64(len(content)),
			IsDir:     false,
			ModTime:   time.Now().UTC(),
		},
	})
	searchResp, err := http.Get(ts.URL + "/api/search?q=hello")
	if err != nil {
		t.Fatalf("search request failed: %v", err)
	}
	var searchResults []db.IndexedFile
	_ = json.NewDecoder(searchResp.Body).Decode(&searchResults)
	searchResp.Body.Close()
	if len(searchResults) != 1 || searchResults[0].Name != "hello.txt" {
		t.Fatalf("expected 1 search result 'hello.txt', got %v", searchResults)
	}

	// 6. Test Trash & Restore
	trashBody, _ := json.Marshal(map[string]string{"account_id": "test_drive", "path": "/hello.txt"})
	trashResp, err := http.Post(ts.URL+"/api/files/trash", "application/json", bytes.NewReader(trashBody))
	if err != nil || trashResp.StatusCode != http.StatusOK {
		t.Fatalf("trash failed, status: %v", trashResp.StatusCode)
	}
	var trashRec db.TrashRecord
	_ = json.NewDecoder(trashResp.Body).Decode(&trashRec)
	trashResp.Body.Close()

	// Restore
	restoreBody, _ := json.Marshal(map[string]string{"trash_id": trashRec.ID})
	restoreResp, err := http.Post(ts.URL+"/api/trash/restore", "application/json", bytes.NewReader(restoreBody))
	if err != nil || restoreResp.StatusCode != http.StatusOK {
		t.Fatalf("restore failed, status: %v", restoreResp.StatusCode)
	}
	restoreResp.Body.Close()

	// 7. Test Audit Trail
	auditResp, err := http.Get(ts.URL + "/api/audit")
	if err != nil {
		t.Fatalf("audit log request failed: %v", err)
	}
	var auditEvents []db.AuditEvent
	_ = json.NewDecoder(auditResp.Body).Decode(&auditEvents)
	auditResp.Body.Close()
	if len(auditEvents) < 3 {
		t.Fatalf("expected audit events recorded, got %d", len(auditEvents))
	}
}

func TestPortHunting(t *testing.T) {
	// Bind to an arbitrary available port
	l1, port1, err := server.FindAvailableListener("127.0.0.1", 5210, 5220)
	if err != nil {
		t.Fatalf("failed to find initial listener: %v", err)
	}
	defer l1.Close()

	// Scanning starting at the same port should automatically pick the next port
	l2, port2, err := server.FindAvailableListener("127.0.0.1", port1, port1+10)
	if err != nil {
		t.Fatalf("failed to find second listener: %v", err)
	}
	defer l2.Close()

	if port2 <= port1 {
		t.Fatalf("expected port hunting to advance port: port1=%d, port2=%d", port1, port2)
	}
}

func TestUpdateAccount(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloudgate_update_account_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := server.NewServer(database, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	setupTestAuth(t, database, ts.URL)

	// 1. Seed initial RemoteAccount
	initialAcc := db.RemoteAccount{
		ID:         "acc_123",
		Provider:   "s3",
		Name:       "Old Bucket Account",
		RootFolder: "/",
		Status:     "connected",
		QuotaTotal: 1000,
		QuotaUsed:  200,
	}
	if err := database.SaveAccount(initialAcc); err != nil {
		t.Fatalf("failed to save account: %v", err)
	}

	// Index a file for this account
	_ = database.IndexFiles([]db.IndexedFile{
		{AccountID: "acc_123", Path: "/doc.pdf", Name: "doc.pdf", Size: 10, ModTime: time.Now().UTC()},
	})

	// 2. Successful PATCH to update name, root_folder, and quota_total
	updatePayload := map[string]any{
		"name":        "New Primary S3",
		"root_folder": "/backups",
		"quota_total": 5000,
	}
	payloadBytes, _ := json.Marshal(updatePayload)
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/accounts/acc_123", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got code: %v, err: %v", resp.StatusCode, err)
	}

	var updated db.RemoteAccount
	_ = json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()

	if updated.Name != "New Primary S3" {
		t.Fatalf("expected updated name, got %s", updated.Name)
	}
	if updated.RootFolder != "/backups" {
		t.Fatalf("expected updated root folder, got %s", updated.RootFolder)
	}
	if updated.QuotaTotal != 5000 {
		t.Fatalf("expected quota 5000, got %d", updated.QuotaTotal)
	}

	// 3. Verify MetadataIndex was cleared due to root_folder change
	searchReq, _ := http.Get(ts.URL + "/api/search?q=doc")
	var searchResults []db.IndexedFile
	_ = json.NewDecoder(searchReq.Body).Decode(&searchResults)
	searchReq.Body.Close()
	if len(searchResults) != 0 {
		t.Fatalf("expected index to be cleared after root_folder change, got %d results", len(searchResults))
	}

	// 4. Test validation: Empty name should return 400
	badNameReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/accounts/acc_123", bytes.NewReader([]byte(`{"name":"   "}`)))
	badNameResp, _ := http.DefaultClient.Do(badNameReq)
	if badNameResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty name, got %d", badNameResp.StatusCode)
	}
	badNameResp.Body.Close()

	// 5. Test validation: Negative quota should return 400
	badQuotaReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/accounts/acc_123", bytes.NewReader([]byte(`{"quota_total":-100}`)))
	badQuotaResp, _ := http.DefaultClient.Do(badQuotaReq)
	if badQuotaResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative quota, got %d", badQuotaResp.StatusCode)
	}
	badQuotaResp.Body.Close()

	// 6. Test 404 on unknown account ID
	notFoundReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/accounts/non_existent", bytes.NewReader([]byte(`{"name":"X"}`)))
	notFoundResp, _ := http.DefaultClient.Do(notFoundReq)
	if notFoundResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown account, got %d", notFoundResp.StatusCode)
	}
	notFoundResp.Body.Close()

	// 7. Verify Audit Log contains "update"
	auditResp, _ := http.Get(ts.URL + "/api/audit")
	var auditEvents []db.AuditEvent
	_ = json.NewDecoder(auditResp.Body).Decode(&auditEvents)
	auditResp.Body.Close()

	foundAudit := false
	for _, ev := range auditEvents {
		if ev.Action == "update" && ev.AccountID == "acc_123" {
			foundAudit = true
			break
		}
	}
	if !foundAudit {
		t.Fatalf("expected audit event with action 'update' for acc_123")
	}
}

func TestUnifiedStorageHubAndFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloudgate_hub_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := server.NewServer(database, nil)
	memDriver := storage.NewMemDriver("acc_hub", "gdrive", 1024*1024)
	srv.RegisterDriver(memDriver)
	pool := storage.NewStoragePool("all_pool", "Virtual StoragePool", []storage.Driver{memDriver})
	srv.RegisterPool(pool)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	setupTestAuth(t, database, ts.URL)

	// 1. Create Account
	_ = database.SaveAccount(db.RemoteAccount{
		ID:         "acc_hub",
		Provider:   "gdrive",
		Name:       "Hub Account",
		RootFolder: "/",
		Status:     "connected",
		QuotaTotal: 1024 * 1024,
		QuotaUsed:  100,
		Enabled:    true,
	})

	// 2. Test Connection on account
	testResp, err := http.Post(ts.URL+"/api/accounts/acc_hub/test", "application/json", nil)
	if err != nil || testResp.StatusCode != http.StatusOK {
		t.Fatalf("test connection failed: %v", err)
	}
	var testResult map[string]any
	_ = json.NewDecoder(testResp.Body).Decode(&testResult)
	testResp.Body.Close()
	if testResult["success"] != true {
		t.Fatalf("expected test connection success=true, got %v", testResult)
	}

	// 3. Toggle account (disable integration)
	toggleResp, err := http.Post(ts.URL+"/api/accounts/acc_hub/toggle", "application/json", bytes.NewReader([]byte(`{"enabled":false}`)))
	if err != nil || toggleResp.StatusCode != http.StatusOK {
		t.Fatalf("toggle failed: %v", err)
	}
	var toggled db.RemoteAccount
	_ = json.NewDecoder(toggleResp.Body).Decode(&toggled)
	toggleResp.Body.Close()
	if toggled.Enabled != false || toggled.Status != "disabled" {
		t.Fatalf("expected account to be disabled, got %v", toggled)
	}

	// 4. Sync Now
	syncResp, err := http.Post(ts.URL+"/api/storage/sync", "application/json", nil)
	if err != nil || syncResp.StatusCode != http.StatusOK {
		t.Fatalf("sync storage failed: %v", err)
	}
	var syncResult map[string]any
	_ = json.NewDecoder(syncResp.Body).Decode(&syncResult)
	syncResp.Body.Close()
	if syncResult["status"] != "synced" {
		t.Fatalf("expected synced status, got %v", syncResult)
	}

	// 5. Starred Files
	starReq, _ := json.Marshal(map[string]any{
		"account_id": "acc_hub",
		"path":       "/important.doc",
		"name":       "important.doc",
		"size":       2048,
		"is_dir":     false,
	})
	starResp, err := http.Post(ts.URL+"/api/files/starred", "application/json", bytes.NewReader(starReq))
	if err != nil || starResp.StatusCode != http.StatusCreated {
		t.Fatalf("add starred failed: %v", err)
	}
	starResp.Body.Close()

	getStarredResp, _ := http.Get(ts.URL + "/api/files/starred")
	var starredList []db.StarredRecord
	_ = json.NewDecoder(getStarredResp.Body).Decode(&starredList)
	getStarredResp.Body.Close()
	if len(starredList) != 1 || starredList[0].Path != "/important.doc" {
		t.Fatalf("expected 1 starred file, got %v", starredList)
	}

	// 6. Recent Files
	recentResp, err := http.Get(ts.URL + "/api/files/recent")
	if err != nil || recentResp.StatusCode != http.StatusOK {
		t.Fatalf("get recent failed: %v", err)
	}
	recentResp.Body.Close()

	// 7. Share Link
	shareResp, err := http.Get(ts.URL + "/api/files/share?account_id=acc_hub&path=/important.doc")
	if err != nil || shareResp.StatusCode != http.StatusOK {
		t.Fatalf("get share link failed: %v", err)
	}
	var shareResult map[string]string
	_ = json.NewDecoder(shareResp.Body).Decode(&shareResult)
	shareResp.Body.Close()
	if shareResult["share_url"] == "" {
		t.Fatalf("expected non-empty share_url, got %v", shareResult)
	}
}

func TestAccountPrincipalAndDefaultNaming(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cloudgate-principal-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := db.Open(tmpDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	srv := server.NewServer(database, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	setupTestAuth(t, database, ts.URL)

	// 1. S3 without name should default to "Amazon S3 (bucket-test)" and have Email = "bucket-test"
	s3Payload := map[string]any{
		"provider":   "s3",
		"bucket":     "bucket-test",
		"access_key": "AKIA123",
		"secret_key": "sec123",
	}
	s3Bytes, _ := json.Marshal(s3Payload)
	resp, err := http.Post(ts.URL+"/api/accounts", "application/json", bytes.NewReader(s3Bytes))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create s3 account failed: %v, status: %d", err, resp.StatusCode)
	}
	var s3Acc db.RemoteAccount
	_ = json.NewDecoder(resp.Body).Decode(&s3Acc)
	resp.Body.Close()

	if s3Acc.Name != "Amazon S3 (bucket-test)" {
		t.Errorf("expected name 'Amazon S3 (bucket-test)', got '%s'", s3Acc.Name)
	}
	if s3Acc.Email != "bucket-test" {
		t.Errorf("expected email/principal 'bucket-test', got '%s'", s3Acc.Email)
	}

	// 2. Mega with username/email without custom name
	megaPayload := map[string]any{
		"provider": "mega",
		"username": "user@mega.nz",
		"password": "password123",
	}
	megaBytes, _ := json.Marshal(megaPayload)
	resp, err = http.Post(ts.URL+"/api/accounts", "application/json", bytes.NewReader(megaBytes))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create mega account failed: %v, status: %d", err, resp.StatusCode)
	}
	var megaAcc db.RemoteAccount
	_ = json.NewDecoder(resp.Body).Decode(&megaAcc)
	resp.Body.Close()

	if megaAcc.Name != "MEGA (user@mega.nz)" {
		t.Errorf("expected name 'MEGA (user@mega.nz)', got '%s'", megaAcc.Name)
	}
	if megaAcc.Email != "user@mega.nz" {
		t.Errorf("expected email 'user@mega.nz', got '%s'", megaAcc.Email)
	}

	// 3. WebDAV with explicit custom name
	webdavPayload := map[string]any{
		"provider": "webdav",
		"name":     "My Private Cloud",
		"url":      "https://nextcloud.example.com/remote.php/dav",
		"username": "admin",
		"password": "secretpassword",
	}
	webdavBytes, _ := json.Marshal(webdavPayload)
	resp, err = http.Post(ts.URL+"/api/accounts", "application/json", bytes.NewReader(webdavBytes))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create webdav account failed: %v, status: %d", err, resp.StatusCode)
	}
	var webdavAcc db.RemoteAccount
	_ = json.NewDecoder(resp.Body).Decode(&webdavAcc)
	resp.Body.Close()

	if webdavAcc.Name != "My Private Cloud" {
		t.Errorf("expected custom name 'My Private Cloud', got '%s'", webdavAcc.Name)
	}
	if webdavAcc.Email != "admin@nextcloud.example.com" {
		t.Errorf("expected email 'admin@nextcloud.example.com', got '%s'", webdavAcc.Email)
	}

	// 4. Filen with email & api_key without custom name
	filenPayload := map[string]any{
		"provider": "filen",
		"email":    "user@filen.io",
		"password": "filenpassword",
		"api_key":  "mock_filen_key",
	}
	filenBytes, _ := json.Marshal(filenPayload)
	resp, err = http.Post(ts.URL+"/api/accounts", "application/json", bytes.NewReader(filenBytes))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create filen account failed: %v, status: %d", err, resp.StatusCode)
	}
	var filenAcc db.RemoteAccount
	_ = json.NewDecoder(resp.Body).Decode(&filenAcc)
	resp.Body.Close()

	if filenAcc.Name != "Filen (user@filen.io)" {
		t.Errorf("expected name 'Filen (user@filen.io)', got '%s'", filenAcc.Name)
	}
	if filenAcc.Email != "user@filen.io" {
		t.Errorf("expected email 'user@filen.io', got '%s'", filenAcc.Email)
	}

	// 4. Test auto-reconcile upgrading generic fallback names on sync
	// Seed an account with generic name and empty email
	genericAcc := db.RemoteAccount{
		ID:         "acc_generic_gdrive",
		Provider:   "gdrive",
		Name:       "GOOGLE Account",
		RootFolder: "/",
		Status:     "connected",
		QuotaTotal: 1000,
		QuotaUsed:  100,
	}
	_ = database.SaveAccount(genericAcc)

	// Create a driver that returns UserEmail
	mockDriver := storage.NewRcloneAdapter("gdrive", "acc_generic_gdrive", "", "", "", "", "testuser@gmail.com", "Test User")
	srv.RegisterDriver(mockDriver)

	syncResp, err := http.Post(ts.URL+"/api/storage/sync", "application/json", nil)
	if err != nil || syncResp.StatusCode != http.StatusOK {
		t.Fatalf("sync failed: %v, status: %d", err, syncResp.StatusCode)
	}
	syncResp.Body.Close()

	reconciled, err := database.GetAccount("acc_generic_gdrive")
	if err != nil {
		t.Fatalf("failed to get reconciled account: %v", err)
	}
	if reconciled.Email != "testuser@gmail.com" {
		t.Errorf("expected reconciled email 'testuser@gmail.com', got '%s'", reconciled.Email)
	}
	if reconciled.Name != "Google Drive (testuser@gmail.com)" {
		t.Errorf("expected reconciled name 'Google Drive (testuser@gmail.com)', got '%s'", reconciled.Name)
	}
}



