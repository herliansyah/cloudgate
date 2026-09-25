package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/storage"
)

func TestRcloneAdapter_Factory(t *testing.T) {
	creds := `{"client_id":"cid","client_secret":"cs","access_token":"at","refresh_token":"rt"}`
	for _, prov := range []string{"onedrive", "dropbox"} {
		drv, err := storage.NewRcloneDriver(prov, "acc_"+prov, creds)
		if err != nil {
			t.Fatalf("factory failed for %s: %v", prov, err)
		}
		if drv.Provider() != prov {
			t.Errorf("expected provider %s got %s", prov, drv.Provider())
		}
		if drv.ID() != "acc_"+prov {
			t.Errorf("expected id acc_%s got %s", prov, drv.ID())
		}
	}
	if _, err := storage.NewRcloneDriver("", "id", creds); err == nil {
		t.Error("expected error for empty provider")
	}
}

func TestRcloneAdapter_OneDrive_AboutAndList(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/me/drive":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"quota": map[string]any{"total": 10737418240, "used": 2147483648, "remaining": 8589934592},
			})
		case "/v1.0/me/drive/root/children":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": []any{
					map[string]any{"name": "doc.odt", "size": 1234, "file": map[string]any{}, "lastModifiedDateTime": "2026-09-24T10:00:00Z"},
					map[string]any{"name": "pics", "folder": map[string]any{}, "lastModifiedDateTime": "2026-09-24T10:01:00Z"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found: " + r.URL.Path})
		}
	}))
	defer ts.Close()

	drv := storage.NewRcloneAdapter("onedrive", "acc_od", "cid", "cs", "dummy_token", "", "user@outlook.com", "Outlook User")
	drv.SetBaseURL(ts.URL)
	ctx := context.Background()

	quota, err := drv.About(ctx)
	if err != nil {
		t.Fatalf("About failed: %v", err)
	}
	if quota.Total != 10737418240 || quota.Used != 2147483648 {
		t.Errorf("unexpected quota %+v", quota)
	}

	files, err := drv.List(ctx, "/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Name != "doc.odt" || files[0].IsDir {
		t.Errorf("expected doc.odt file, got %+v", files[0])
	}
	if !files[1].IsDir || files[1].Name != "pics" {
		t.Errorf("expected pics folder, got %+v", files[1])
	}
}

func TestRcloneAdapter_OneDrive_PutGetDelete(t *testing.T) {
	var putPath, delPath string
	var putBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PUT" && r.URL.Path == "/v1.0/me/drive/root:/hello.txt:/content":
			putPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			putBody = b
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "hello.txt"})
		case r.Method == "GET" && r.URL.Path == "/v1.0/me/drive/root:/hello.txt:/content":
			w.Header().Set("Content-Length", "5")
			_, _ = w.Write([]byte("hello"))
		case r.Method == "DELETE" && r.URL.Path == "/v1.0/me/drive/root:/hello.txt":
			delPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()
	drv := storage.NewRcloneAdapter("onedrive", "acc_od", "cid", "cs", "tok", "", "", "")
	drv.SetBaseURL(ts.URL)
	ctx := context.Background()
	if err := drv.Put(ctx, "/hello.txt", bytes.NewReader([]byte("hello")), 5); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if putPath != "/v1.0/me/drive/root:/hello.txt:/content" || string(putBody) != "hello" {
		t.Errorf("unexpected put %s %s", putPath, string(putBody))
	}
	rc, info, err := drv.Get(ctx, "/hello.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "hello" || info.Name != "hello.txt" {
		t.Errorf("unexpected get %+v %s", info, string(b))
	}
	if err := drv.Delete(ctx, "/hello.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if delPath == "" {
		t.Error("expected delete to be called")
	}
}

func TestRcloneAdapter_Dropbox_AboutAndList(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2/users/get_space_usage":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"used": 5000000,
				"allocation": map[string]any{"allocated": 21474836480},
			})
		case "/2/files/list_folder":
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries": []any{
					map[string]any{".tag": "file", "name": "a.txt", "path_display": "/a.txt", "size": 42, "client_modified": "2026-09-24T10:00:00Z"},
					map[string]any{".tag": "folder", "name": "fld", "path_display": "/fld", "client_modified": "2026-09-24T10:00:00Z"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()
	drv := storage.NewRcloneAdapter("dropbox", "acc_db", "cid", "cs", "tok", "", "user@dropbox.com", "DB User")
	drv.SetBaseURL(ts.URL)
	ctx := context.Background()
	q, err := drv.About(ctx)
	if err != nil {
		t.Fatalf("About failed: %v", err)
	}
	if q.Total != 21474836480 || q.Used != 5000000 {
		t.Errorf("unexpected quota %+v", q)
	}
	files, err := drv.List(ctx, "/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2, got %d", len(files))
	}
	if files[0].Name != "a.txt" || files[0].IsDir {
		t.Errorf("expected file a.txt, got %+v", files[0])
	}
	if !files[1].IsDir {
		t.Errorf("expected folder, got %+v", files[1])
	}
}

func TestRcloneAdapter_Dropbox_PutGetMoveMkdir(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2/files/upload":
			// content server — check Dropbox-API-Arg
			arg := r.Header.Get("Dropbox-API-Arg")
			var m map[string]any
			_ = json.Unmarshal([]byte(arg), &m)
			if m["path"] != "/b.txt" {
				t.Errorf("unexpected upload path %v", m["path"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "b.txt"})
		case "/2/files/download":
			arg := r.Header.Get("Dropbox-API-Arg")
			if arg != `{"path":"/b.txt"}` {
				t.Errorf("unexpected download arg %s", arg)
			}
			w.Header().Set("Dropbox-Api-Result", `{"name":"b.txt","size":3}`)
			_, _ = w.Write([]byte("hi!"))
		case "/2/files/move_v2":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case "/2/files/create_folder_v2":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case "/2/files/delete_v2":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(r.URL.Path))
		}
	}))
	defer ts.Close()
	drv := storage.NewRcloneAdapter("dropbox", "acc_db", "cid", "cs", "tok", "", "", "")
	drv.SetBaseURL(ts.URL)
	ctx := context.Background()
	if err := drv.Put(ctx, "/b.txt", bytes.NewReader([]byte("hi!")), 3); err != nil {
		t.Fatalf("Put %v", err)
	}
	rc, info, err := drv.Get(ctx, "/b.txt")
	if err != nil {
		t.Fatalf("Get %v", err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "hi!" || info.Size != 3 {
		t.Errorf("unexpected get %s %+v", string(b), info)
	}
	if err := drv.Move(ctx, "/b.txt", "/c.txt"); err != nil {
		t.Fatalf("Move %v", err)
	}
	if err := drv.Mkdir(ctx, "/newfld"); err != nil {
		t.Fatalf("Mkdir %v", err)
	}
	if err := drv.Delete(ctx, "/b.txt"); err != nil {
		t.Fatalf("Delete %v", err)
	}
}

func TestRcloneAdapter_Mega_Basic(t *testing.T) {
	creds := `{"username":"user@example.com","password":"secretpassword"}`
	drv, err := storage.NewRcloneDriver("mega", "acc_mega_test", creds)
	if err != nil {
		t.Fatalf("failed to create mega driver: %v", err)
	}
	if drv.Provider() != "mega" {
		t.Errorf("expected mega, got %s", drv.Provider())
	}
	ctx := context.Background()
	// Test put/get in fallback/in-memory mode for offline test
	drv.SetBaseURL("http://127.0.0.1:9999")
	if err := drv.Put(ctx, "/test.txt", bytes.NewReader([]byte("megadata")), 8); err != nil {
		t.Fatalf("put failed: %v", err)
	}
	files, err := drv.List(ctx, "/")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("expected at least 1 file, got 0")
	}
	rc, info, err := drv.Get(ctx, "/test.txt")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "megadata" || info.Name != "test.txt" {
		t.Errorf("unexpected content: %s", string(data))
	}
}

