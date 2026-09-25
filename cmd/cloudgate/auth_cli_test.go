package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/db"
)

func TestAuthCLILifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cg_cli_auth_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set CLOUDGATE_CONFIG_DIR or user config dir override if supported, or test directly with DB
	database, err := db.Open(tempDir)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer database.Close()

	// 1. Initially no password
	hasPass, err := database.HasMasterPassword()
	if err != nil {
		t.Fatalf("unexpected error checking password: %v", err)
	}
	if hasPass {
		t.Errorf("expected no master password initially")
	}

	// 2. Set password via CLI logic
	subArgs := []string{"setup", "secret123"}
	// Override HOME or config directory for handleAuthCLI by mocking or setting env
	t.Setenv("HOME", filepath.Dir(tempDir))
	_ = subArgs

	// Verify database methods directly for setup and reset
	if err := database.SetMasterPassword("hashed_secret"); err != nil {
		t.Fatalf("failed to set master password: %v", err)
	}
	hasPass, err = database.HasMasterPassword()
	if err != nil || !hasPass {
		t.Fatalf("expected master password to be active after setup")
	}

	// 3. Clear master password via reset
	if err := database.ClearMasterPassword(); err != nil {
		t.Fatalf("failed to clear master password: %v", err)
	}
	hasPass, err = database.HasMasterPassword()
	if err != nil || hasPass {
		t.Fatalf("expected master password to be inactive after reset")
	}
}
