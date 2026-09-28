package web_test

import (
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/herliansyah/cloudgate/pkg/config"
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

func TestFilenUIComponentsAndGuide(t *testing.T) {
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
		`id: 'filen'`,
		`filen: '#2764eb'`,
		`<option value="filen">`,
		`id="filenExtraFields"`,
		`id="filenUser"`,
		`id="filenPass"`,
		`id="filenApiKey"`,
		`export-api-key.sh`,
		`4. Filen (Zero-Knowledge End-to-End Encrypted Cloud)`,
		`id="filen-logo"`,
	}

	for _, marker := range requiredMarkers {
		if !strings.Contains(content, marker) {
			t.Errorf("expected embedded index.html to contain Filen marker %q, but was not found", marker)
		}
	}
}

func TestEmbeddedAppVersionConsistency(t *testing.T) {
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

	// Ensure stale version strings like 0.2.0 are not present in the UI
	if strings.Contains(content, "M3 v0.2.0") || strings.Contains(content, `id="aboutVersion">0.2.0<`) {
		t.Errorf("embedded index.html still contains stale version 0.2.0; expected version to match config.AppVersion (%q)", config.AppVersion)
	}

	expectedBadge := "M3 v" + config.AppVersion
	expectedAbout := `id="aboutVersion">` + config.AppVersion + `<`
	if !strings.Contains(content, expectedBadge) {
		t.Errorf("embedded index.html does not contain header version badge %q", expectedBadge)
	}
	if !strings.Contains(content, expectedAbout) {
		t.Errorf("embedded index.html does not contain about version tag %q", expectedAbout)
	}
}

func TestDocumentationProvidersAndApiSync(t *testing.T) {
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

	// Extract renderDocs function body
	startDocs := strings.Index(content, "function renderDocs()")
	if startDocs == -1 {
		t.Fatalf("function renderDocs() not found in index.html")
	}
	endDocs := strings.Index(content[startDocs:], "function promptDisconnectAccount")
	if endDocs == -1 {
		t.Fatalf("end of renderDocs not found in index.html")
	}
	docsContent := content[startDocs : startDocs+endDocs]

	// Ensure renderDocs includes guides for all 16 supported providers
	expectedProviderGuides := []string{
		"Google Drive",
		"Microsoft OneDrive",
		"Dropbox",
		"Box",
		"pCloud",
		"Yandex Disk",
		"Koofr",
		"Mega",
		"Filen",
		"Backblaze B2",
		"PikPak",
		"SFTP",
		"SMB",
		"Proton Drive",
		"S3",
		"WebDAV",
	}

	for _, p := range expectedProviderGuides {
		if !strings.Contains(docsContent, p) {
			t.Errorf("expected renderDocs in index.html to contain documentation for provider %q, but was not found", p)
		}
	}

	// Ensure renderDocs contains reference to /api/providers endpoint
	if !strings.Contains(docsContent, "/api/providers") {
		t.Errorf("expected renderDocs in index.html to contain /api/providers endpoint reference")
	}

	// Ensure hardcoded localhost:8080 is NOT present in renderDocs OAuth callbacks
	if strings.Contains(docsContent, "http://localhost:8080/api/auth/") {
		t.Errorf("embedded index.html renderDocs contains hardcoded port 8080 redirect URI")
	}
}
