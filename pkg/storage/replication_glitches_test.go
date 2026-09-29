package storage

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/herliansyah/cloudgate/pkg/db"
)

func TestReplicateFolder_ModTimeDetection(t *testing.T) {
	ctx := context.Background()
	src := NewMemDriver("src", "mock", 10000)
	dst := NewMemDriver("dst", "mock", 10000)

	// In destination: file exists with same size (5 bytes), but old mod time
	_ = dst.Put(ctx, "/dst/doc.txt", bytes.NewReader([]byte("hello")), 5)

	// Wait a small moment to ensure mod time difference
	time.Sleep(20 * time.Millisecond)

	// In source: file was modified with new content ("world", also 5 bytes!) and newer mod time
	_ = src.Put(ctx, "/src/doc.txt", bytes.NewReader([]byte("world")), 5)

	// Run ReplicateFolder
	_, itemsProcessed, _, err := ReplicateFolder(ctx, src, "/src", dst, "/dst", false, nil, nil)
	if err != nil {
		t.Fatalf("ReplicateFolder failed: %v", err)
	}

	// Should have copied 1 file because ModTime is newer
	if itemsProcessed != 1 {
		t.Errorf("expected 1 item copied due to newer ModTime, got %d", itemsProcessed)
	}

	// Verify destination content is "world"
	rc, _, err := dst.Get(ctx, "/dst/doc.txt")
	if err != nil {
		t.Fatalf("dst Get failed: %v", err)
	}
	defer rc.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(rc)
	if buf.String() != "world" {
		t.Errorf("expected 'world', got %q", buf.String())
	}
}

func TestReplicateFolder_MirrorOrphanDirectories(t *testing.T) {
	ctx := context.Background()
	tmpDir, _ := os.MkdirTemp("", "cloudgate-rep-mirror-*")
	defer os.RemoveAll(tmpDir)
	database, _ := db.Open(tmpDir)
	defer database.Close()
	trashMgr := NewTrashManager(database)

	src := NewMemDriver("src", "mock", 10000)
	dst := NewMemDriver("dst", "mock", 10000)

	// Source has /src/keep.txt
	_ = src.Put(ctx, "/src/keep.txt", bytes.NewReader([]byte("keep")), 4)

	// Destination has /dst/keep.txt and /dst/old_sub/orphan.txt
	_ = dst.Put(ctx, "/dst/keep.txt", bytes.NewReader([]byte("keep")), 4)
	_ = dst.Put(ctx, "/dst/old_sub/orphan.txt", bytes.NewReader([]byte("orphan")), 6)

	// Mirror replicate
	_, _, _, err := ReplicateFolder(ctx, src, "/src", dst, "/dst", true, trashMgr, nil)
	if err != nil {
		t.Fatalf("ReplicateFolder mirror failed: %v", err)
	}

	// The orphan directory /dst/old_sub should not exist anymore
	entries, err := dst.List(ctx, "/dst")
	if err != nil {
		t.Fatalf("dst List failed: %v", err)
	}
	for _, e := range entries {
		if e.Path == "/dst/old_sub" || e.Name == "old_sub" {
			t.Errorf("expected orphaned directory /dst/old_sub to be removed in mirror mode")
		}
	}
}

func TestCleanRelPath_PrefixBoundaries(t *testing.T) {
	// Sibling directory that starts with same characters
	// Base is /data, but target is /data_other/file.txt
	// This is NOT inside /data!
	rel := cleanRelPath("/data", "/data_other/file.txt")
	if rel == "_other/file.txt" {
		t.Errorf("cleanRelPath should not match /data_other as a subpath of /data, got %q", rel)
	}

	// Valid subpath
	relValid := cleanRelPath("/data", "/data/sub/file.txt")
	if relValid != "sub/file.txt" {
		t.Errorf("expected 'sub/file.txt', got %q", relValid)
	}
}
