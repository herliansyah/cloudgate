package server_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/server"
	"github.com/herliansyah/cloudgate/pkg/storage"
)

func TestFilePreviewAndFolderNavigation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cloudgate_preview_test_*")
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
	memDriver := storage.NewMemDriver("test_drive", "gdrive", 10*1024*1024)
	srv.RegisterDriver(memDriver)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Upload a PDF file
	uploadFile(t, ts.URL, "test_drive", "/documents", "sample.pdf", "%PDF-1.4 sample content")
	// Upload an Image file
	uploadFile(t, ts.URL, "test_drive", "/images", "photo.png", "\x89PNG\r\n\x1a\nfake image bytes")
	// Upload a text file
	uploadFile(t, ts.URL, "test_drive", "/", "notes.txt", "meeting notes content")

	// 1. Verify inline download for PDF file sets proper Content-Type and inline disposition
	pdfResp, err := http.Get(ts.URL + "/api/files/download?account_id=test_drive&path=/documents/sample.pdf&inline=true")
	if err != nil {
		t.Fatalf("failed to request PDF preview: %v", err)
	}
	defer pdfResp.Body.Close()

	disp := pdfResp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(disp, "inline") {
		t.Errorf("expected Content-Disposition inline, got %s", disp)
	}
	cType := pdfResp.Header.Get("Content-Type")
	if !strings.HasPrefix(cType, "application/pdf") {
		t.Errorf("expected Content-Type application/pdf, got %s", cType)
	}

	// 2. Verify inline download for PNG file
	imgResp, err := http.Get(ts.URL + "/api/files/download?account_id=test_drive&path=/images/photo.png&inline=true")
	if err != nil {
		t.Fatalf("failed to request image preview: %v", err)
	}
	defer imgResp.Body.Close()

	disp = imgResp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(disp, "inline") {
		t.Errorf("expected Content-Disposition inline, got %s", disp)
	}
	cType = imgResp.Header.Get("Content-Type")
	if !strings.HasPrefix(cType, "image/png") {
		t.Errorf("expected Content-Type image/png, got %s", cType)
	}
}

func uploadFile(t *testing.T, baseURL, accountID, targetPath, filename, content string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = io.WriteString(part, content)
	_ = writer.Close()

	req, _ := http.NewRequest("POST", baseURL+"/api/files/upload?account_id="+accountID+"&path="+targetPath, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload failed for %s, status: %v, err: %v", filename, resp.StatusCode, err)
	}
	resp.Body.Close()
}
