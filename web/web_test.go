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

func TestEmbeddedModalHierarchyAndButtonConsistency(t *testing.T) {
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

	// 1. Verify balanced div tags
	openDivs := strings.Count(content, "<div")
	closeDivs := strings.Count(content, "</div")
	if openDivs != closeDivs {
		t.Errorf("unbalanced <div> tags in index.html: %d open vs %d close", openDivs, closeDivs)
	}

	// 2. Verify modals are not nested inside addAccountModal
	addModalIdx := strings.Index(content, `id="addAccountModal"`)
	editModalIdx := strings.Index(content, `id="editAccountModal"`)
	disconnectModalIdx := strings.Index(content, `id="disconnectAccountModal"`)
	if addModalIdx == -1 || editModalIdx == -1 || disconnectModalIdx == -1 {
		t.Fatalf("required modal IDs not found in index.html")
	}

	sliceBeforeEdit := content[addModalIdx:editModalIdx]
	if strings.Count(sliceBeforeEdit, "<div") > strings.Count(sliceBeforeEdit, "</div") {
		t.Errorf("editAccountModal is nested inside addAccountModal (div depth not reset)")
	}

	sliceBeforeDisconnect := content[addModalIdx:disconnectModalIdx]
	if strings.Count(sliceBeforeDisconnect, "<div") > strings.Count(sliceBeforeDisconnect, "</div") {
		t.Errorf("disconnectAccountModal is nested inside addAccountModal (div depth not reset)")
	}

	// 3. Verify no redundant "+ Add Storage" text next to plus icon
	if strings.Contains(content, "+ Add Storage") {
		t.Errorf("found redundant '+ Add Storage' text next to plus icon")
	}

	// 4. Verify formatPercent and Storage Hub usage progress bar
	if !strings.Contains(content, "formatPercent") {
		t.Errorf("formatPercent helper function missing from index.html")
	}
	if !strings.Contains(content, "storageHubUsageBar") {
		t.Errorf("storageHubUsageBar missing from Storage Hub in index.html")
	}

	// 5. Verify provider-card-footer uses clean single action and action-dropdown-menu
	if !strings.Contains(content, "action-dropdown-menu") {
		t.Errorf("action-dropdown-menu missing from index.html")
	}
	if !strings.Contains(content, "status-indicator-dot") {
		t.Errorf("status-indicator-dot missing from index.html")
	}
}

