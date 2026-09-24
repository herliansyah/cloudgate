package storage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/storage"
)

func TestGDriveDriver_ResolveFolderAndList(t *testing.T) {
	// Mock Google Drive v3 API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")

		// Handle folder resolution for "docs"
		if q == "name = 'docs' and 'root' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "folder_docs_id", "name": "docs"},
				},
			})
			return
		}

		// Handle list files in 'root'
		if q == "'root' in parents and trashed = false" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "folder_docs_id", "name": "docs", "mimeType": "application/vnd.google-apps.folder", "size": "0", "modifiedTime": "2026-09-24T10:00:00Z"},
					{"id": "file_root_id", "name": "root_file.txt", "mimeType": "text/plain", "size": "12", "modifiedTime": "2026-09-24T10:00:00Z"},
				},
			})
			return
		}

		// Handle list files inside 'docs' folder
		if q == "'folder_docs_id' in parents and trashed = false" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": []map[string]any{
					{"id": "file_doc_id", "name": "manual.pdf", "mimeType": "application/pdf", "size": "1024", "modifiedTime": "2026-09-24T10:05:00Z"},
				},
			})
			return
		}

		// Default empty
		_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{}})
	}))
	defer ts.Close()

	// Use test server URL in custom client or transport
	driver := storage.NewGDriveDriver("test_acc", "client_id", "client_secret", "dummy_token", "", "user@example.com", "Test User")
	driver.SetBaseURL(ts.URL)

	ctx := context.Background()

	// 1. List root folder
	rootFiles, err := driver.List(ctx, "/")
	if err != nil {
		t.Fatalf("failed to list root files: %v", err)
	}
	if len(rootFiles) != 2 {
		t.Fatalf("expected 2 files in root, got %d", len(rootFiles))
	}
	if !rootFiles[0].IsDir || rootFiles[0].Name != "docs" {
		t.Errorf("expected docs to be a directory, got %+v", rootFiles[0])
	}

	// 2. List subfolder /docs
	subFiles, err := driver.List(ctx, "/docs")
	if err != nil {
		t.Fatalf("failed to list subfolder files: %v", err)
	}
	if len(subFiles) != 1 {
		t.Fatalf("expected 1 file in /docs, got %d", len(subFiles))
	}
	if subFiles[0].Name != "manual.pdf" || subFiles[0].Path != "/docs/manual.pdf" {
		t.Errorf("expected /docs/manual.pdf, got %+v", subFiles[0])
	}
}
