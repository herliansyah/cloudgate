package storage

import (
	"testing"
)

// TestOAuthDriverInitialExpiryIsZero verifies that storage adapters initialize
// tokenExpiry to zero time so that stale OAuth tokens loaded from DB on startup
// trigger an immediate token refresh before the first API request.
func TestOAuthDriverInitialExpiryIsZero(t *testing.T) {
	gdrv := NewGDriveDriver("acc1", "cid", "csec", "stale_token", "refresh_token", "", "user")
	if !gdrv.tokenExpiry.IsZero() {
		t.Fatalf("expected gdrive driver tokenExpiry to be zero on construction, got %v", gdrv.tokenExpiry)
	}

	radapter := NewRcloneAdapter("onedrive", "acc2", "cid", "csec", "stale_token", "refresh_token", "", "user")
	if !radapter.tokenExpiry.IsZero() {
		t.Fatalf("expected rclone adapter tokenExpiry to be zero on construction, got %v", radapter.tokenExpiry)
	}
}
