package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// ponytail: check that Windows resource COFF objects are present and contain .rsrc section
func TestWindowsResourceSyso(t *testing.T) {
	targets := []string{
		"rsrc_windows_amd64.syso",
		"rsrc_windows_arm64.syso",
	}

	for _, target := range targets {
		path := filepath.Join(".", target)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected Windows resource file %s to exist: %v", target, err)
		}

		if len(data) < 512 {
			t.Errorf("%s is unexpectedly small: %d bytes", target, len(data))
		}

		// Verify COFF header contains .rsrc section name
		if !bytes.Contains(data, []byte(".rsrc")) {
			t.Errorf("expected %s to contain .rsrc section header", target)
		}
	}
}
