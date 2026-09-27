package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		latest   string
		current  string
		expected bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.9.9", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"0.9.0", "0.10.0", false},
		{"v0.2.0", "v0.1.0", true},
		{"", "0.1.0", false},
	}

	for _, tt := range tests {
		cleanL := strings.TrimPrefix(tt.latest, "v")
		cleanC := strings.TrimPrefix(tt.current, "v")
		got := isNewerVersion(cleanL, cleanC)
		if got != tt.expected {
			t.Errorf("isNewerVersion(%q, %q) = %v; want %v", tt.latest, tt.current, got, tt.expected)
		}
	}
}

func TestParseChecksumsAndFind(t *testing.T) {
	raw := `
# Official Release Checksums
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  cloudgate_linux_amd64
a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3 *cloudgate_darwin_arm64
`
	m := parseChecksums(strings.NewReader(raw))
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}

	h1 := findExpectedChecksum(m, "cloudgate_linux_amd64")
	if h1 != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("unexpected hash for linux_amd64: %s", h1)
	}

	h2 := findExpectedChecksum(m, "cloudgate_darwin_arm64")
	if h2 != "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3" {
		t.Errorf("unexpected hash for darwin_arm64: %s", h2)
	}

	h3 := findExpectedChecksum(m, "nonexistent")
	if h3 != "" {
		t.Errorf("expected empty hash for nonexistent asset, got %s", h3)
	}
}

func TestGetChangelog(t *testing.T) {
	ch := GetChangelog()
	if !strings.Contains(ch, "Changelog") {
		t.Errorf("expected GetChangelog to contain 'Changelog', got: %s", ch)
	}
}

func TestApplyUpdate_ChecksumStrictVerification(t *testing.T) {
	ctx := context.Background()

	// 1. Missing checksum URL must fail
	err := ApplyUpdate(ctx, "http://localhost/binary", "")
	if err == nil || !strings.Contains(err.Error(), "strict verification") {
		t.Fatalf("expected strict verification error when checksumURL is empty, got: %v", err)
	}

	// 2. Mismatched checksum must fail
	dummyBin := []byte("this is a dummy binary file")
	hasher := sha256.New()
	hasher.Write(dummyBin)
	validHash := hex.EncodeToString(hasher.Sum(nil))
	invalidHash := "0000000000000000000000000000000000000000000000000000000000000000"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/binary":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(dummyBin)
		case "/checksums-invalid.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "%s  binary\n", invalidHash)
		case "/checksums-valid.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "%s  binary\n", validHash)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	err = ApplyUpdate(ctx, ts.URL+"/binary", ts.URL+"/checksums-invalid.txt")
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Fatalf("expected verification failed error, got: %v", err)
	}
}

func TestReplaceBinary(t *testing.T) {
	tempDir := t.TempDir()
	origFile := filepath.Join(tempDir, "cloudgate")
	newFile := filepath.Join(tempDir, "cloudgate.new")

	if err := os.WriteFile(origFile, []byte("version 1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("version 2"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := replaceBinary(newFile, origFile); err != nil {
		t.Fatalf("replaceBinary failed: %v", err)
	}

	content, err := os.ReadFile(origFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "version 2" {
		t.Errorf("expected 'version 2', got %q", string(content))
	}
}
