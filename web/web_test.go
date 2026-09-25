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

func TestGatewayAuthUIComponents(t *testing.T) {
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

	requiredAuthMarkers := []string{
		"btnGatewayLock",
		"gatewayLockOverlay",
		"setupMasterPasswordModal",
		"changeMasterPasswordModal",
		"function checkGatewayAuthStatus",
		"function showLockScreen",
		"function showSetupScreen",
		"function showRemoteLockedScreen",
		"function submitGatewayUnlock",
		"function lockGateway",
		"function submitSetupMasterPassword",
		"function submitChangeMasterPassword",
		"function submitFirstRunSetup",
	}

	for _, marker := range requiredAuthMarkers {
		if !strings.Contains(content, marker) {
			t.Errorf("expected embedded index.html to contain GatewayAuth marker %q, but was not found", marker)
		}
	}
}

func TestDualLanguageUIAndDocumentation(t *testing.T) {
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

	requiredI18nMarkers := []string{
		`id="btnLang"`,
		`id="langLabel"`,
		"function initLang",
		"function toggleLanguage",
		"function setLanguage",
		"function applyTranslations",
		"function t(",
		"const I18N =",
		"cloudgate-lang",
		"data-i18n=",
		"data-i18n-title=",
	}

	for _, marker := range requiredI18nMarkers {
		if !strings.Contains(content, marker) {
			t.Errorf("expected embedded index.html to contain i18n marker %q, but was not found", marker)
		}
	}
}

func TestGatewayAuthBilingualSupport(t *testing.T) {
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

	requiredGatewayKeys := []string{
		"gateway.cardTitle",
		"gateway.descActive",
		"gateway.descInactive",
		"gateway.setupMasterPassword",
		"gateway.changePassword",
		"gateway.firstRunTitle",
		"gateway.remoteLockedTitle",
	}

	for _, key := range requiredGatewayKeys {
		if !strings.Contains(content, key) {
			t.Errorf("expected embedded index.html to contain GatewayAuth translation key %q, but was not found", key)
		}
	}
}

func TestSnackbarActionButtonContrastAndClarity(t *testing.T) {
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

	// 1. Must use inverse-primary color for high contrast on inverse-surface background
	if strings.Contains(content, `style="color:var(--md-sys-color-primary);font-size:0.82rem;font-weight:700;height:auto;padding:4px 8px;border:none;background:transparent;cursor:pointer;min-width:auto;flex-shrink:0;" onclick="this.parentElement.remove()">OK</button>`) {
		t.Errorf("found low-contrast primary color on snackbar action button; must use var(--md-sys-color-inverse-primary)")
	}

	if !strings.Contains(content, "var(--md-sys-color-inverse-primary)") {
		t.Errorf("expected snackbar action button to utilize var(--md-sys-color-inverse-primary)")
	}

	if !strings.Contains(content, "snackbar-action") {
		t.Errorf("expected dedicated snackbar-action class for clean crisp action button styling")
	}
}

func TestCapacityCardBilingualPersistence(t *testing.T) {
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

	// 1. capacityText must not have data-i18n="nav.quotaLoading", which clobbers loaded stats on language switch
	if strings.Contains(content, `id="capacityText" data-i18n="nav.quotaLoading"`) {
		t.Errorf("capacityText has data-i18n='nav.quotaLoading' which clobbers loaded storage stats when language is switched")
	}

	// 2. updateCapacityCard helper must exist and update stats bilingual display
	if !strings.Contains(content, "function updateCapacityCard()") {
		t.Errorf("updateCapacityCard function missing from index.html")
	}

	// 3. updateCapacityCard must be wired to language changes
	if !strings.Contains(content, "updateCapacityCard();") {
		t.Errorf("updateCapacityCard() call missing from language change flow or stats loading")
	}
}


