package storage_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/storage"
)

func TestStoragePoolAndTransfer(t *testing.T) {
	ctx := context.Background()

	// 1. Test Capacity-Aware Round Robin
	d1 := storage.NewMemDriver("acc_1", "gdrive", 100) // 100 bytes free
	d2 := storage.NewMemDriver("acc_2", "onedrive", 1000) // 1000 bytes free
	d3 := storage.NewMemDriver("acc_3", "dropbox", 1000) // 1000 bytes free
	d3.SetConnected(false) // Offline

	pool := storage.NewStoragePool("pool_1", "My Unified Pool", []storage.Driver{d1, d2, d3})

	// Writing 200 bytes: d1 only has 100 bytes (skip), d2 has 1000 bytes (chosen)
	content := bytes.Repeat([]byte("A"), 200)
	allocatedID, err := pool.Write(ctx, "/docs/file1.txt", bytes.NewReader(content), 200)
	if err != nil {
		t.Fatalf("failed to write 200-byte file to pool: %v", err)
	}
	if allocatedID != "acc_2" {
		t.Fatalf("expected file to be placed on acc_2, got %s", allocatedID)
	}

	// Verify file is on d2
	rc, info, err := d2.Get(ctx, "/docs/file1.txt")
	if err != nil {
		t.Fatalf("expected file on d2: %v", err)
	}
	rc.Close()
	if info.Size != 200 {
		t.Fatalf("expected file size 200, got %d", info.Size)
	}

	// 2. Test In-Memory Streaming Copy & Safe Move
	// Copy file1.txt from d2 to d1 (small enough? 200 bytes won't fit d1 because d1 has 100 bytes quota)
	d1Quota, _ := d1.About(ctx)
	if d1Quota.Free >= 200 {
		t.Fatalf("d1 should not have 200 bytes free")
	}

	// Create d4 with enough space
	d4 := storage.NewMemDriver("acc_4", "s3", 500)
	err = storage.CopyFile(ctx, d2, "/docs/file1.txt", d4, "/backup/file1_backup.txt")
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}

	// Verify target exists on d4
	rcBackup, infoBackup, err := d4.Get(ctx, "/backup/file1_backup.txt")
	if err != nil {
		t.Fatalf("failed to get backup file on d4: %v", err)
	}
	rcBackup.Close()
	if infoBackup.Size != 200 {
		t.Fatalf("expected size 200, got %d", infoBackup.Size)
	}

	// Test Safe Move: Move from d4 to d2 as /docs/moved.txt
	err = storage.MoveFile(ctx, d4, "/backup/file1_backup.txt", d2, "/docs/moved.txt")
	if err != nil {
		t.Fatalf("safe move failed: %v", err)
	}
	// Verify source was deleted on d4
	_, _, err = d4.Get(ctx, "/backup/file1_backup.txt")
	if err == nil {
		t.Fatalf("expected source on d4 to be deleted after move")
	}
	// Verify target exists on d2
	rcMoved, _, err := d2.Get(ctx, "/docs/moved.txt")
	if err != nil {
		t.Fatalf("expected moved file on d2: %v", err)
	}
	rcMoved.Close()

	// 3. Test Trash Manager & Deterministic Auto-Restore
	tempDir, err := os.MkdirTemp("", "cloudgate_trash_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	trashMgr := storage.NewTrashManager(database)

	// Move /docs/file1.txt on d2 to trash
	trashRec, err := trashMgr.MoveToTrash(ctx, d2, "/docs/file1.txt")
	if err != nil {
		t.Fatalf("failed to move to trash: %v", err)
	}
	if trashRec.OriginalPath != "/docs/file1.txt" {
		t.Fatalf("unexpected original path in trash record: %s", trashRec.OriginalPath)
	}

	// Verify original file is gone from original path
	_, _, err = d2.Get(ctx, "/docs/file1.txt")
	if err == nil {
		t.Fatalf("expected file to be gone from original path on d2")
	}

	// Verify file is in remote trash
	rcTrash, _, err := d2.Get(ctx, trashRec.RemoteTrashPath)
	if err != nil {
		t.Fatalf("expected file to exist in remote trash path %s: %v", trashRec.RemoteTrashPath, err)
	}
	rcTrash.Close()

	// Restore from trash
	err = trashMgr.Restore(ctx, d2, trashRec.ID)
	if err != nil {
		t.Fatalf("failed to restore file from trash: %v", err)
	}

	// Verify file is back at original path
	rcRestored, _, err := d2.Get(ctx, "/docs/file1.txt")
	if err != nil {
		t.Fatalf("expected file to be restored to /docs/file1.txt: %v", err)
	}
	data, _ := io.ReadAll(rcRestored)
	rcRestored.Close()
	if !bytes.Equal(data, content) {
		t.Fatalf("restored data does not match original")
	}

	// Verify trash record was deleted from database
	trashList, _ := database.GetTrashRecords()
	if len(trashList) != 0 {
		t.Fatalf("expected 0 trash records in db after restore, got %d", len(trashList))
	}
}
