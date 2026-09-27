package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/herliansyah/cloudgate/pkg/db"
)

func TestSSRFValidation(t *testing.T) {
	blockedURLs := []string{
		"http://127.0.0.1:8080/secret",
		"http://localhost:5210/api",
		"http://192.168.1.10/admin",
		"http://10.0.0.5/",
		"http://172.16.0.1/status",
		"http://169.254.169.254/latest/meta-data/",
		"ftp://example.com/file.iso",
		"file:///etc/passwd",
	}

	for _, u := range blockedURLs {
		if _, err := ValidateSSRF(u); err == nil {
			t.Errorf("expected SSRF validation to block %s, but it passed", u)
		}
	}

	validURLs := []string{
		"https://example.com/file.zip",
		"http://download.documentfoundation.org/libreoffice/stable/file.tar.gz",
	}

	for _, u := range validURLs {
		if _, err := ValidateSSRF(u); err != nil {
			t.Errorf("expected %s to pass validation, got err: %v", u, err)
		}
	}
}

func TestResolveIngestFilename(t *testing.T) {
	u, _ := url.Parse("https://example.com/files/archive.zip?token=123")
	if name := ResolveIngestFilename(nil, u, "custom.zip"); name != "custom.zip" {
		t.Errorf("expected custom.zip, got %s", name)
	}

	// Content-Disposition priority over URL path
	resp := &http.Response{
		Header: http.Header{
			"Content-Disposition": []string{`attachment; filename="server-file.tar.gz"`},
		},
	}
	if name := ResolveIngestFilename(resp, u, ""); name != "server-file.tar.gz" {
		t.Errorf("expected server-file.tar.gz, got %s", name)
	}

	// URL path fallback
	if name := ResolveIngestFilename(nil, u, ""); name != "archive.zip" {
		t.Errorf("expected archive.zip, got %s", name)
	}
}

func TestFolderTransfer_MemDriver(t *testing.T) {
	ctx := context.Background()
	src := NewMemDriver("src", "mock", 10000)
	dst := NewMemDriver("dst", "mock", 10000)

	_ = src.Put(ctx, "/folder/a.txt", bytes.NewReader([]byte("file a")), 6)
	_ = src.Put(ctx, "/folder/sub/b.txt", bytes.NewReader([]byte("file b"), ), 6)

	pBytes, itemsP, errLog, err := TransferFolder(ctx, src, "/folder", dst, "/backup", false, nil)
	if err != nil {
		t.Fatalf("TransferFolder copy failed: %v", err)
	}
	if len(errLog) > 0 {
		t.Fatalf("unexpected transfer errors: %v", errLog)
	}
	if itemsP != 2 || pBytes != 12 {
		t.Errorf("expected 2 items and 12 bytes, got %d items and %d bytes", itemsP, pBytes)
	}

	// Verify target files exist
	rc, info, err := dst.Get(ctx, "/backup/a.txt")
	if err != nil {
		t.Fatalf("dst file a.txt missing: %v", err)
	}
	rc.Close()
	if info.Size != 6 {
		t.Errorf("expected size 6, got %d", info.Size)
	}

	rc2, _, err := dst.Get(ctx, "/backup/sub/b.txt")
	if err != nil {
		t.Fatalf("dst file sub/b.txt missing: %v", err)
	}
	rc2.Close()
}

func TestReplicateFolder_MemDriver(t *testing.T) {
	ctx := context.Background()
	tmpDir, _ := os.MkdirTemp("", "cloudgate-replicate-test-*")
	defer os.RemoveAll(tmpDir)
	database, _ := db.Open(tmpDir)
	defer database.Close()
	trashMgr := NewTrashManager(database)

	src := NewMemDriver("src", "mock", 10000)
	dst := NewMemDriver("dst", "mock", 10000)

	_ = src.Put(ctx, "/src/f1.txt", bytes.NewReader([]byte("v1")), 2)
	_ = src.Put(ctx, "/src/f2.txt", bytes.NewReader([]byte("hello")), 5)

	// In destination: f1 has old content ("v0", 2 bytes), orphan.txt exists
	_ = dst.Put(ctx, "/dst/f1.txt", bytes.NewReader([]byte("old content")), 11)
	_ = dst.Put(ctx, "/dst/orphan.txt", bytes.NewReader([]byte("to be deleted")), 13)

	// Replicate with mirror=true
	pBytes, itemsP, errLog, err := ReplicateFolder(ctx, src, "/src", dst, "/dst", true, trashMgr, nil)
	if err != nil {
		t.Fatalf("ReplicateFolder failed: %v", err)
	}
	if len(errLog) > 0 {
		t.Fatalf("unexpected replicate errors: %v", errLog)
	}
	if itemsP != 2 {
		t.Errorf("expected 2 items copied, got %d (pBytes: %d)", itemsP, pBytes)
	}

	// orphan.txt should be moved to trash
	_, _, err = dst.Get(ctx, "/dst/orphan.txt")
	if err == nil {
		t.Errorf("expected orphan.txt to be removed from /dst")
	}

	trashRecords, _ := database.GetTrashRecords()
	if len(trashRecords) != 1 || trashRecords[0].FileName != "orphan.txt" {
		t.Errorf("expected orphan.txt in trash records, got: %+v", trashRecords)
	}
}

func TestTaskManager_Lifecycle(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "cloudgate-tm-test-*")
	defer os.RemoveAll(tmpDir)
	database, _ := db.Open(tmpDir)
	defer database.Close()
	trashMgr := NewTrashManager(database)

	src := NewMemDriver("acc-1", "mock", 10000)
	dst := NewMemDriver("acc-2", "mock", 10000)

	_ = src.Put(context.Background(), "/data.txt", bytes.NewReader([]byte("async transfer test")), 19)

	getDriver := func(accID string) (Driver, error) {
		if accID == "acc-1" {
			return src, nil
		}
		if accID == "acc-2" {
			return dst, nil
		}
		return nil, fmt.Errorf("unknown driver: %s", accID)
	}

	tm := NewTaskManager(database, trashMgr, getDriver)
	if err := tm.Start(); err != nil {
		t.Fatalf("failed to start TaskManager: %v", err)
	}
	defer tm.Stop()

	task := &db.StorageTask{
		ID:              "async-task-1",
		Type:            "transfer",
		SourceAccountID: "acc-1",
		SourcePath:      "/data.txt",
		TargetAccountID: "acc-2",
		TargetPath:      "/target.txt",
		Status:          "pending",
	}

	if err := tm.Enqueue(task); err != nil {
		t.Fatalf("failed to enqueue task: %v", err)
	}

	// Poll until completed
	deadline := time.Now().Add(5 * time.Second)
	var completedTask *db.StorageTask
	for time.Now().Before(deadline) {
		tRecord, err := database.GetTask("async-task-1")
		if err == nil && tRecord != nil && (tRecord.Status == "completed" || tRecord.Status == "failed") {
			completedTask = tRecord
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if completedTask == nil || completedTask.Status != "completed" {
		t.Fatalf("task did not complete in time: %+v", completedTask)
	}

	// Verify target file exists on dst driver
	rc, info, err := dst.Get(context.Background(), "/target.txt")
	if err != nil {
		t.Fatalf("target file not found on dst driver: %v", err)
	}
	content, _ := io.ReadAll(rc)
	rc.Close()
	if string(content) != "async transfer test" || info.Size != 19 {
		t.Errorf("unexpected content: %s", string(content))
	}
}

func TestRemoteIngest_LocalMockServer(t *testing.T) {
	// Stand up a mock test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="cloud-file.bin"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("stream-payload-content"))
	}))
	defer ts.Close()

	dst := NewMemDriver("dst", "mock", 10000)

	// Since httptest runs on 127.0.0.1, standard ValidateSSRF would reject it.
	// For testing the direct streaming pipeline logic:
	u, _ := url.Parse(ts.URL)
	finalFilename := ResolveIngestFilename(nil, u, "custom-download.bin")
	if finalFilename != "custom-download.bin" {
		t.Errorf("expected custom-download.bin, got %s", finalFilename)
	}

	// Test direct HTTP get with driver Put
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http get failed: %v", err)
	}
	defer resp.Body.Close()

	if err := dst.Put(context.Background(), "/downloads/cloud-file.bin", resp.Body, resp.ContentLength); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	rc, info, err := dst.Get(context.Background(), "/downloads/cloud-file.bin")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "stream-payload-content" || info.Size != 22 {
		t.Errorf("unexpected downloaded content: %s", string(b))
	}
}
