package pkg_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/herliansyah/cloudgate/pkg/config"
	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/vault"
)

func TestCoreFoundation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloudgate_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Setenv("CLOUDGATE_CONFIG_DIR", tempDir)

	// 1. Check Single-Instance Lock
	lock1, _, err := config.AcquireInstanceLock(5210)
	if err != nil {
		t.Fatalf("failed to acquire initial lock: %v", err)
	}

	_, existing, err := config.AcquireInstanceLock(5211)
	if err == nil {
		t.Fatalf("expected second lock acquisition to fail, but succeeded")
	}
	if existing == nil || existing.Port != 5210 {
		t.Fatalf("expected existing instance port 5210, got %v", existing)
	}

	config.ReleaseInstanceLock(lock1)

	// Verify lock can be re-acquired after release
	lock2, _, err := config.AcquireInstanceLock(5212)
	if err != nil {
		t.Fatalf("failed to re-acquire lock after release: %v", err)
	}
	config.ReleaseInstanceLock(lock2)

	// 2. Check Encrypted Vault
	secretData := []byte("top_secret_oauth_refresh_token_12345")
	passphrase := "MySecureMasterPass123!"

	encrypted, err := vault.Encrypt(secretData, passphrase)
	if err != nil {
		t.Fatalf("vault encryption failed: %v", err)
	}

	// Decrypt with correct password
	decrypted, err := vault.Decrypt(encrypted, passphrase)
	if err != nil {
		t.Fatalf("vault decryption failed: %v", err)
	}
	if !bytes.Equal(decrypted, secretData) {
		t.Fatalf("decrypted data mismatch: got %s, want %s", string(decrypted), string(secretData))
	}

	// Decrypt with wrong password must fail
	_, err = vault.Decrypt(encrypted, "WrongPassword!")
	if err == nil {
		t.Fatalf("expected decryption with wrong password to fail")
	}

	// 3. Check SQLite DB & Atomic 100-Event Rolling Audit Log
	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Insert 125 audit events
	for i := 1; i <= 125; i++ {
		err := database.RecordAudit(
			"upload",
			fmt.Sprintf("file_%d.txt", i),
			"gdrive_1",
			fmt.Sprintf("details %d", i),
			"success",
			int64(i*10),
		)
		if err != nil {
			t.Fatalf("failed to record audit event %d: %v", i, err)
		}
	}

	// Verify count is strictly capped at 100
	count, err := database.CountAuditEvents()
	if err != nil {
		t.Fatalf("failed to count audit events: %v", err)
	}
	if count != 100 {
		t.Fatalf("expected exactly 100 audit events due to rolling trigger, got %d", count)
	}

	// Verify newest is 125 and oldest is 26
	events, err := database.GetRecentAuditEvents()
	if err != nil {
		t.Fatalf("failed to get audit events: %v", err)
	}
	if len(events) != 100 {
		t.Fatalf("expected 100 events, got %d", len(events))
	}
	if events[0].Target != "file_125.txt" {
		t.Fatalf("expected newest event to be file_125.txt, got %s", events[0].Target)
	}
	if events[99].Target != "file_26.txt" {
		t.Fatalf("expected oldest preserved event to be file_26.txt, got %s", events[99].Target)
	}

	// Check Trash Record Mapping
	trash := db.TrashRecord{
		ID:              "trash_1",
		AccountID:       "acc_onedrive",
		OriginalPath:    "/Documents/report.docx",
		RemoteTrashPath: "/.cloudgate_trash/report_1234.docx",
		FileName:        "report.docx",
		Size:            1024,
		DeletedAt:       time.Now().UTC(),
	}
	if err := database.AddTrashRecord(trash); err != nil {
		t.Fatalf("failed to add trash record: %v", err)
	}
	records, err := database.GetTrashRecords()
	if err != nil || len(records) != 1 {
		t.Fatalf("expected 1 trash record, got %d (err: %v)", len(records), err)
	}
	if records[0].OriginalPath != "/Documents/report.docx" {
		t.Fatalf("original path mismatch: %s", records[0].OriginalPath)
	}
	if err := database.DeleteTrashRecord("trash_1"); err != nil {
		t.Fatalf("failed to delete trash record: %v", err)
	}
	recordsAfter, _ := database.GetTrashRecords()
	if len(recordsAfter) != 0 {
		t.Fatalf("expected 0 trash records after deletion, got %d", len(recordsAfter))
	}
}
