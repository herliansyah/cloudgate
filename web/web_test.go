package web_test

import (
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/herliansyah/cloudgate/web"
)

func TestEmbeddedWebDashboardAndControls(t *testing.T) {
	fsys, err := web.GetFS()
	if err != nil {
		t.Fatalf("web.GetFS() failed: %v", err)
	}

	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("failed to open embedded index.html: %v", err)
	}
	defer f.Close()

	contentBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read embedded index.html: %v", err)
	}
	content := string(contentBytes)

	requiredMarkers := []string{
		"navItemDashboard",
		"pool-hero-card",
		"provider-grid",
		"disconnectAccountModal",
		"disconnectConfirmInput",
		"fileControlsBar",
		"filterChips",
		"sortSelect",
		"groupSelect",
		"file-card-preview",
		"function getFileType",
		"function getFileIconSvg",
	}

	for _, marker := range requiredMarkers {
		if !strings.Contains(content, marker) {
			t.Errorf("expected embedded index.html to contain marker %q, but was not found", marker)
		}
	}
}

func TestEmbeddedJavaScriptSyntax(t *testing.T) {
	fsys, err := web.GetFS()
	if err != nil {
		t.Fatalf("web.GetFS() failed: %v", err)
	}

	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("failed to open embedded index.html: %v", err)
	}
	defer f.Close()

	contentBytes, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read embedded index.html: %v", err)
	}
	content := string(contentBytes)

	// Extract script content
	startIdx := strings.Index(content, "<script>")
	endIdx := strings.LastIndex(content, "</script>")
	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		t.Fatalf("unable to find <script> tags in embedded index.html")
	}
	scriptContent := content[startIdx+len("<script>") : endIdx]

	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node executable not found in PATH, skipping JS syntax check")
		return
	}

	cmd := exec.Command(nodePath, "--check")
	cmd.Stdin = strings.NewReader(scriptContent)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("embedded JavaScript syntax check failed: %v\nOutput: %s", err, string(out))
	}
}

