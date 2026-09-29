package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/server"
	"github.com/herliansyah/cloudgate/pkg/storage"
)

func TestTasksAPI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cloudgate-server-tasks-test-*")
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
	defer srv.Close()

	// Register two mock drivers
	drv1 := storage.NewMemDriver("acc-1", "mock", 10000)
	drv2 := storage.NewMemDriver("acc-2", "mock", 10000)
	srv.RegisterDriver(drv1)
	srv.RegisterDriver(drv2)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	setupTestAuth(t, database, ts.URL)

	// 1. GET /api/tasks initially empty
	resp, err := http.Get(ts.URL + "/api/tasks")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/tasks failed: %v, status: %d", err, resp.StatusCode)
	}
	var tasks []db.StorageTask
	_ = json.NewDecoder(resp.Body).Decode(&tasks)
	resp.Body.Close()
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks initially, got %d", len(tasks))
	}

	// 2. POST /api/tasks/transfer
	transferReq := map[string]any{
		"source_account_id": "acc-1",
		"source_path":      "/docs/spec.pdf",
		"target_account_id": "acc-2",
		"target_path":      "/backup/spec.pdf",
		"is_dir":           false,
		"is_move":          false,
	}
	transferBody, _ := json.Marshal(transferReq)
	resp, err = http.Post(ts.URL+"/api/tasks/transfer", "application/json", bytes.NewReader(transferBody))
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/tasks/transfer failed: %v, status: %d", err, resp.StatusCode)
	}
	var createdTask db.StorageTask
	_ = json.NewDecoder(resp.Body).Decode(&createdTask)
	resp.Body.Close()
	if createdTask.ID == "" || createdTask.Type != "transfer" {
		t.Errorf("unexpected created task: %+v", createdTask)
	}

	// 3. POST /api/tasks/ingest with SSRF blocked URL should fail with 400
	ssrfReq := map[string]any{
		"url":               "http://127.0.0.1:8080/internal",
		"target_account_id": "acc-1",
		"target_path":       "/downloads",
	}
	ssrfBody, _ := json.Marshal(ssrfReq)
	resp, err = http.Post(ts.URL+"/api/tasks/ingest", "application/json", bytes.NewReader(ssrfBody))
	if err != nil {
		t.Fatalf("POST /api/tasks/ingest failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for SSRF URL, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. POST /api/tasks/ingest with valid URL
	validIngestReq := map[string]any{
		"url":               "https://example.com/archive.zip",
		"target_account_id": "acc-1",
		"target_path":       "/downloads",
		"custom_name":       "archive-2026.zip",
	}
	validIngestBody, _ := json.Marshal(validIngestReq)
	resp, err = http.Post(ts.URL+"/api/tasks/ingest", "application/json", bytes.NewReader(validIngestBody))
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST valid /api/tasks/ingest failed: %v, status: %d", err, resp.StatusCode)
	}
	var ingestTask db.StorageTask
	_ = json.NewDecoder(resp.Body).Decode(&ingestTask)
	resp.Body.Close()
	if ingestTask.Type != "ingest" {
		t.Errorf("expected ingest type, got %s", ingestTask.Type)
	}
	if ingestTask.IsDir {
		t.Errorf("expected is_dir to be false when custom_name is provided")
	}

	// 5. POST /api/tasks/replicate - same account loop should fail with 400
	loopReq := map[string]any{
		"source_account_id": "acc-1",
		"source_path":       "/",
		"target_account_id": "acc-1",
		"target_path":       "/backup",
		"mirror":            true,
	}
	loopBody, _ := json.Marshal(loopReq)
	resp, err = http.Post(ts.URL+"/api/tasks/replicate", "application/json", bytes.NewReader(loopBody))
	if err != nil {
		t.Fatalf("POST loop /api/tasks/replicate failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for same-account recursive replication, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 5b. POST /api/tasks/replicate - cross-account replication should succeed
	repReq := map[string]any{
		"source_account_id": "acc-1",
		"source_path":       "/photos",
		"target_account_id": "acc-2",
		"target_path":       "/photos_mirror",
		"mirror":            true,
	}
	repBody, _ := json.Marshal(repReq)
	resp, err = http.Post(ts.URL+"/api/tasks/replicate", "application/json", bytes.NewReader(repBody))
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/tasks/replicate failed: %v, status: %d", err, resp.StatusCode)
	}
	var repTask db.StorageTask
	_ = json.NewDecoder(resp.Body).Decode(&repTask)
	resp.Body.Close()
	if repTask.Type != "replicate" || !repTask.Mirror {
		t.Errorf("expected replicate mirror task, got %+v", repTask)
	}

	// 6. POST /api/tasks/{id}/cancel
	resp, err = http.Post(ts.URL+"/api/tasks/"+repTask.ID+"/cancel", "application/json", nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel task failed: %v, status: %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	cancelled, _ := database.GetTask(repTask.ID)
	if cancelled.Status != "cancelled" {
		t.Errorf("expected cancelled status, got %s", cancelled.Status)
	}

	// 7. POST /api/tasks/{id}/retry
	resp, err = http.Post(ts.URL+"/api/tasks/"+repTask.ID+"/retry", "application/json", nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("retry task failed: %v, status: %d", err, resp.StatusCode)
	}
	var retriedTask db.StorageTask
	_ = json.NewDecoder(resp.Body).Decode(&retriedTask)
	resp.Body.Close()
	if retriedTask.Status != "pending" {
		t.Errorf("expected pending after retry, got %s", retriedTask.Status)
	}

	// 8. DELETE /api/tasks/finished
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/tasks/finished", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/tasks/finished failed: %v, status: %d", err, resp.StatusCode)
	}
	resp.Body.Close()
}
