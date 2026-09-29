package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
)

func TestResolveIngestFilename_RFC5987AndMime(t *testing.T) {
	u, _ := url.Parse("https://example.com/download?id=123")

	// 1. Content-Disposition with RFC 5987 filename*
	respRFC := &http.Response{
		Header: http.Header{
			"Content-Disposition": []string{`attachment; filename*=UTF-8''my%20report%202026.pdf`},
		},
	}
	got := ResolveIngestFilename(respRFC, u, "")
	if got != "my report 2026.pdf" {
		t.Errorf("expected 'my report 2026.pdf', got %q", got)
	}

	// 2. URL without filename, but Content-Type header present
	avatarURL, _ := url.Parse("https://example.com/avatar")
	respContentType := &http.Response{
		Header: http.Header{
			"Content-Type": []string{"image/png"},
		},
	}
	gotImg := ResolveIngestFilename(respContentType, avatarURL, "")
	if path.Ext(gotImg) != ".png" && gotImg != "avatar.png" {
		t.Errorf("expected filename ending in .png, got %q", gotImg)
	}

	// 3. HTTP redirect: resp.Request.URL has the real redirected filename
	redirectedURL, _ := url.Parse("https://cdn.example.com/files/release-v2.tar.gz")
	respRedirect := &http.Response{
		Request: &http.Request{URL: redirectedURL},
		Header:  http.Header{},
	}
	gotRedirect := ResolveIngestFilename(respRedirect, u, "")
	if gotRedirect != "release-v2.tar.gz" {
		t.Errorf("expected 'release-v2.tar.gz' from redirected request URL, got %q", gotRedirect)
	}
}

func TestRemoteIngest_TruncatedStream(t *testing.T) {
	AllowLocalForTesting = true
	defer func() { AllowLocalForTesting = false }()

	// Server claims 100 bytes, but sends only 20 bytes
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short truncated data"))
	}))
	defer ts.Close()

	dst := NewMemDriver("dst", "mock", 10000)
	_, _, err := RemoteIngestStream(context.Background(), ts.URL, "test.bin", dst, "/downloads", nil)
	if err == nil {
		t.Fatalf("expected error for truncated download, got nil")
	}
}

func TestTaskManager_IngestCustomNameWithoutExtension(t *testing.T) {
	AllowLocalForTesting = true
	defer func() { AllowLocalForTesting = false }()

	tmpDir, _ := os.MkdirTemp("", "cloudgate-ingest-bug-*")
	defer os.RemoveAll(tmpDir)
	database, _ := db.Open(tmpDir)
	defer database.Close()
	trashMgr := NewTrashManager(database)

	dst := NewMemDriver("acc-1", "mock", 100000)
	getDriver := func(accID string) (Driver, error) {
		return dst, nil
	}

	tm := NewTaskManager(database, trashMgr, getDriver)
	_ = tm.Start()
	defer tm.Stop()

	// Server returning a payload
	payload := []byte("binary payload content")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer ts.Close()

	// Ingest task with TargetPath: "/bin" and CustomName: "mytool" (no extension!)
	// When saved in storage_tasks, TargetPath is "/bin/mytool" and IsDir is false
	task := &db.StorageTask{
		ID:              "ingest-custom-noext",
		Type:            "ingest",
		SourcePath:      ts.URL,
		TargetAccountID: "acc-1",
		TargetPath:      "/bin/mytool", // custom_name "mytool" inside "/bin"
		IsDir:           false,
		Status:          "pending",
	}
	_ = database.CreateTask(task)

	// Directly invoke handleIngest
	err := tm.handleIngest(context.Background(), task)
	if err != nil {
		t.Fatalf("handleIngest failed: %v", err)
	}

	// The file should be saved as "/bin/mytool", NOT as "/bin/mytool/<auto-name>"
	rc, info, err := dst.Get(context.Background(), "/bin/mytool")
	if err != nil {
		t.Fatalf("expected file at /bin/mytool, but got error: %v", err)
	}
	rc.Close()
	if info.IsDir {
		t.Errorf("expected /bin/mytool to be a file, not a directory")
	}

	// Verify database was updated with final TargetPath
	dbTask, err := database.GetTask("ingest-custom-noext")
	if err != nil || dbTask == nil {
		t.Fatalf("failed to fetch task from db: %v", err)
	}
	if dbTask.TargetPath != "/bin/mytool" {
		t.Errorf("expected db task TargetPath to be '/bin/mytool', got %q", dbTask.TargetPath)
	}
}

func TestTaskManager_IngestFolderWithDot(t *testing.T) {
	AllowLocalForTesting = true
	defer func() { AllowLocalForTesting = false }()

	tmpDir, _ := os.MkdirTemp("", "cloudgate-ingest-dot-*")
	defer os.RemoveAll(tmpDir)
	database, _ := db.Open(tmpDir)
	defer database.Close()
	trashMgr := NewTrashManager(database)

	dst := NewMemDriver("acc-1", "mock", 100000)
	getDriver := func(accID string) (Driver, error) {
		return dst, nil
	}

	tm := NewTaskManager(database, trashMgr, getDriver)
	_ = tm.Start()
	defer tm.Stop()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="data.csv"`)
		_, _ = w.Write([]byte("a,b,c\n1,2,3"))
	}))
	defer ts.Close()

	// Ingest task into folder "/v1.0" with no custom name
	// TargetPath is "/v1.0"
	task := &db.StorageTask{
		ID:              "ingest-folder-dot",
		Type:            "ingest",
		SourcePath:      ts.URL,
		TargetAccountID: "acc-1",
		TargetPath:      "/v1.0", // Target directory has a dot!
		IsDir:           true,    // Folder destination!
		Status:          "pending",
	}
	_ = database.CreateTask(task)

	err := tm.handleIngest(context.Background(), task)
	if err != nil {
		t.Fatalf("handleIngest failed: %v", err)
	}

	// File should be saved inside "/v1.0/data.csv", NOT as "/v1.0"
	rc, _, err := dst.Get(context.Background(), "/v1.0/data.csv")
	if err != nil {
		t.Fatalf("expected file at /v1.0/data.csv, but got error: %v", err)
	}
	rc.Close()

	// Check DB target path
	dbTask, _ := database.GetTask("ingest-folder-dot")
	if dbTask.TargetPath != "/v1.0/data.csv" {
		t.Errorf("expected db task TargetPath to be '/v1.0/data.csv', got %q", dbTask.TargetPath)
	}
}
