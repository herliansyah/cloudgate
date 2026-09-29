package web_test

import (
	"io"
	"os/exec"
	"regexp"
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
		// The API key comes from the official Filen CLI, not a piped remote script.
		`filen export-api-key`,
		`https://github.com/FilenCloudDienste/filen-cli`,
		`id="filen-logo"`,
	}

	for _, marker := range requiredMarkers {
		if !strings.Contains(content, marker) {
			t.Errorf("expected embedded index.html to contain Filen marker %q, but was not found", marker)
		}
	}
	if strings.Contains(content, "export-api-key.sh | bash") {
		t.Errorf("Filen guide must not instruct users to pipe a remote script into bash")
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

	expectedBadge := "M3 v" + config.AppVersion
	expectedAbout := `id="aboutVersion">` + config.AppVersion + `<`
	if !strings.Contains(content, expectedBadge) {
		t.Errorf("embedded index.html does not contain header version badge %q", expectedBadge)
	}
	if !strings.Contains(content, expectedAbout) {
		t.Errorf("embedded index.html does not contain about version tag %q", expectedAbout)
	}

	// Verify that the actual embedded tags do not contain any stale/mismatched version
	reBadge := regexp.MustCompile(`<span class="tag" id="appVersionTag">([^<]+)</span>`)
	if match := reBadge.FindStringSubmatch(content); len(match) > 1 {
		if match[1] != expectedBadge {
			t.Errorf("embedded appVersionTag has stale version %q; expected %q", match[1], expectedBadge)
		}
	} else {
		t.Errorf("embedded appVersionTag element not found in index.html")
	}

	reAbout := regexp.MustCompile(`<span id="aboutVersion">([^<]+)</span>`)
	if match := reAbout.FindStringSubmatch(content); len(match) > 1 {
		if match[1] != config.AppVersion {
			t.Errorf("embedded aboutVersion has stale version %q; expected %q", match[1], config.AppVersion)
		}
	} else {
		t.Errorf("embedded aboutVersion element not found in index.html")
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

	// The Docs handbook is generated from PROVIDER_METADATA and must list all 16 providers.
	expectedProviderIDs := []string{
		"gdrive", "onedrive", "dropbox", "box", "pcloud", "yandex", "koofr", "webdav",
		"s3", "b2", "mega", "filen", "pikpak", "protondrive", "sftp", "smb",
	}
	for _, id := range expectedProviderIDs {
		if !strings.Contains(docsContent, "'"+id+"'") {
			t.Errorf("expected Docs handbook to include provider %q", id)
		}
	}
	metaStart := strings.Index(content, "const PROVIDER_METADATA = {")
	if metaStart == -1 {
		t.Fatalf("PROVIDER_METADATA not found")
	}
	metaEnd := strings.Index(content[metaStart:], "function openModal(")
	meta := content[metaStart : metaStart+metaEnd]
	for _, id := range expectedProviderIDs {
		if !strings.Contains(meta, "\n      "+id+": {") {
			t.Errorf("PROVIDER_METADATA missing provider %q", id)
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

func TestEmbeddedEscapeHtmlAndUpdaterCheck(t *testing.T) {
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

	if !strings.Contains(content, "function escapeHtml(") {
		t.Errorf("expected embedded index.html to declare function escapeHtml")
	}

	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node executable not found in PATH, skipping runtime check")
		return
	}

	startIdx := strings.Index(content, "<script>")
	endIdx := strings.LastIndex(content, "</script>")
	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		t.Fatalf("unable to find <script> tags in embedded index.html")
	}
	scriptContent := content[startIdx+len("<script>") : endIdx]

	harness := `
const fs = require("fs");
const script = fs.readFileSync(0, "utf-8");
const vm = require("vm");
const textElem = {};
const ctx = {
  setInterval: () => {},
  window: { addEventListener: () => {}, location: {}, localStorage: { getItem: () => null } },
  document: { getElementById: (id) => id === "updateStatusText" ? textElem : null },
  fetch: async () => ({ ok: true, json: async () => ({ update_available: true, latest_version: "v1.2.0", current_version: "v1.1.0" }) })
};
vm.createContext(ctx);
vm.runInContext(script, ctx);

if (typeof ctx.escapeHtml !== 'function') {
  console.error("escapeHtml is not defined as a function");
  process.exit(1);
}

const escaped = ctx.escapeHtml('<script>alert("x & y")</script>');
const expected = '&lt;script&gt;alert(&quot;x &amp; y&quot;)&lt;/script&gt;';
if (escaped !== expected) {
  console.error("escapeHtml mismatch. Got: " + escaped + " expected: " + expected);
  process.exit(2);
}

ctx.checkAppUpdates().then(() => {
  if (textElem.innerText && textElem.innerText.includes("escapeHtml is not defined")) {
    console.error("checkAppUpdates failed: " + textElem.innerText);
    process.exit(3);
  }
  process.exit(0);
}).catch(err => {
  console.error(err);
  process.exit(4);
});
`
	cmd := exec.Command(nodePath, "-e", harness)
	cmd.Stdin = strings.NewReader(scriptContent)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime check failed: %v\nOutput: %s", err, string(out))
	}
}

func TestEnglishLocalizationCoverage(t *testing.T) {
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

	// 1. Empty files state must not hardcode Indonesian text
	if strings.Contains(content, `<p style="font-weight:700;font-size:1.05rem;">Tidak ada file ditemukan</p>`) {
		t.Errorf("renderRenderableFiles hardcodes Indonesian empty state: 'Tidak ada file ditemukan'")
	}

	// 2. URL Ingest modal must be localized
	if strings.Contains(content, `>URL File (HTTP / HTTPS)</label>`) && !strings.Contains(content, `data-i18n="ingest.urlLabel"`) {
		t.Errorf("URL Ingest modal missing data-i18n for URL input label")
	}
	if strings.Contains(content, `<button class="btn btn-primary" onclick="submitRemoteIngest()">Mulai Unduh</button>`) {
		t.Errorf("URL Ingest modal hardcodes Indonesian submit button 'Mulai Unduh'")
	}

	// 3. Settings & API (renderDocs) must support English localization
	if strings.Contains(content, `>Ringkasan API Endpoints</h3>`) {
		t.Errorf("renderDocs hardcodes Indonesian heading 'Ringkasan API Endpoints'")
	}
	if strings.Contains(content, `<span class="pool-stat-label">Keamanan Kredensial</span>`) {
		t.Errorf("renderDocs hardcodes Indonesian label 'Keamanan Kredensial'")
	}

	// 4. StorageHub table headers must not hardcode Indonesian
	if strings.Contains(content, `<th>Penyedia</th>`) {
		t.Errorf("renderStorageHub hardcodes Indonesian column header 'Penyedia'")
	}
	if strings.Contains(content, `<span style="font-weight:700;font-size:0.95rem;">Penggunaan Kapasitas Teragregasi</span>`) {
		t.Errorf("renderStorageHub hardcodes Indonesian section title 'Penggunaan Kapasitas Teragregasi'")
	}

	// 5. Breadcrumbs must use dynamic t() values instead of hardcoded strings
	if strings.Contains(content, `<span class="breadcrumb-item active">Pusat Penyimpanan</span>`) {
		t.Errorf("updateBreadcrumbs hardcodes Indonesian 'Pusat Penyimpanan'")
	}
	if strings.Contains(content, `<span class="breadcrumb-item active">Pengaturan & API</span>`) {
		t.Errorf("updateBreadcrumbs hardcodes Indonesian 'Pengaturan & API'")
	}

	// 6. Required I18N keys must be present in both EN and ID dictionaries
	requiredKeys := []string{
		"nav.urlIngest",
		"nav.syncFolder",
		"files.emptyTitle",
		"files.emptyFilterDesc",
		"files.emptyUploadDesc",
		"files.parentFolder",
		"files.starTitle",
		"files.shareTitle",
		"files.open",
		"files.preview",
		"files.download",
		"files.trash",
		"bc.dashboard",
		"bc.storageHub",
		"bc.starred",
		"bc.recent",
		"bc.settings",
		"bc.trash",
		"bc.cloudAccount",
		"bc.backUp",
		"ingest.title",
		"ingest.urlLabel",
		"ingest.accountLabel",
		"ingest.dirLabel",
		"ingest.nameLabel",
		"ingest.submit",
		"ingest.noAccount",
		"replicate.title",
		"replicate.sourceAccount",
		"replicate.targetAccount",
		"replicate.submit",
		"replicate.noAccount",
		"docs.endpointsSummary",
		"docs.credSecurity",
		"tasks.title",
		"tasks.empty",
		"tasks.cancel",
		"tasks.retry",
		"hub.fleetTitle",
		"hub.utilization",
		"hub.providerDist",
		"hub.thProvider",
		"hub.thIdentity",
		"hub.thQuota",
		"hub.thStatus",
		"hub.thSync",
		"hub.thIntegration",
		"hub.thActions",
		"sheet.noFileSelected",
		"sheet.noFileDesc",
		"sheet.domainMapping",
		"sheet.remotePath",
		"sheet.fileSize",
		"sheet.lastModified",
		"sheet.loadingAudit",
		"sheet.noActivity",
		"addAcc.title",
		"editAcc.title",
		"disAcc.title",
		"share.title",
		"dlConfirm.title",
	}

	for _, key := range requiredKeys {
		if !strings.Contains(content, "'"+key+"':") {
			t.Errorf("expected I18N dictionary to contain key %q, but was not found", key)
		}
	}
}

func TestBilingualSupportForHandbookStarredAndRecent(t *testing.T) {
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

	// 1. Starred and Recent empty states must not hardcode Indonesian text
	if strings.Contains(content, `>Belum Ada File Berbintang</h3>`) {
		t.Errorf("loadStarredFiles hardcodes Indonesian empty state title: 'Belum Ada File Berbintang'")
	}
	if strings.Contains(content, `>Belum Ada Aktivitas File Terbaru</h3>`) {
		t.Errorf("loadRecentFiles hardcodes Indonesian empty state title: 'Belum Ada Aktivitas File Terbaru'")
	}

	// 2. Starred and Recent empty states must have i18n keys
	requiredEmptyStateKeys := []string{
		"starred.emptyTitle",
		"starred.emptyDesc",
		"starred.explore",
		"starred.loading",
		"recent.emptyTitle",
		"recent.emptyDesc",
		"recent.explore",
		"recent.loading",
	}
	for _, key := range requiredEmptyStateKeys {
		if !strings.Contains(content, "'"+key+"':") {
			t.Errorf("expected I18N dictionary to contain key %q, but was not found", key)
		}
	}

	// 3. Handbook must support English language rendering (not only hardcoded Indonesian steps)
	if strings.Contains(content, `<div><strong>Langkah Pendaftaran:</strong></div>`) {
		t.Errorf("renderDocs handbook hardcodes Indonesian guide step: 'Langkah Pendaftaran:'")
	}
	if strings.Contains(content, `<strong>Panduan Penanganan Kegagalan (Troubleshooting):</strong>`) {
		t.Errorf("renderDocs handbook hardcodes Indonesian troubleshooting title: 'Panduan Penanganan Kegagalan (Troubleshooting):'")
	}
}

func TestEmbeddedFaviconAndBranding(t *testing.T) {
	fsys, err := web.GetFS()
	if err != nil {
		t.Fatalf("web.GetFS() failed: %v", err)
	}

	// Verify favicon.svg exists
	if _, err := fsys.Open("favicon.svg"); err != nil {
		t.Errorf("expected favicon.svg to be present in embedded filesystem, got: %v", err)
	}

	// Verify favicon.ico exists
	if _, err := fsys.Open("favicon.ico"); err != nil {
		t.Errorf("expected favicon.ico to be present in embedded filesystem, got: %v", err)
	}

	// Verify index.html contains favicon link and branding
	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("failed to open embedded index.html: %v", err)
	}
	defer f.Close()

	b, _ := io.ReadAll(f)
	html := string(b)

	if !strings.Contains(html, `rel="icon" type="image/svg+xml" href="/favicon.svg"`) {
		t.Errorf("index.html missing SVG favicon link tag")
	}
	if !strings.Contains(html, `rel="alternate icon" href="/favicon.ico"`) {
		t.Errorf("index.html missing ICO favicon fallback link tag")
	}
	if !strings.Contains(html, `nav-cloud-grad`) {
		t.Errorf("index.html missing Cloudgate vector brand mark gradient")
	}
}

func TestAddProviderGuideBilingualSupport(t *testing.T) {
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

	// In the Add Provider modal, provider onboarding guides must support English:
	// They must not hardcode static Indonesian titles without bilingual branching.
	hardcodedIndonesianSnippets := []string{
		`<span>📘</span> Panduan Pendaftaran Google Drive API (Terbaru):`,
		`<summary style="cursor:pointer;font-weight:700;color:var(--md-sys-color-on-surface);font-size:0.78rem;">⚠️ Panduan Jika Gagal (Troubleshooting)</summary>`,
		`<span>📘</span> Panduan Pendaftaran Microsoft Azure / Entra ID (Ketentuan Terbaru):`,
		`<span>📘</span> Panduan Pendaftaran Dropbox Developer Console:`,
		`<span>📘</span> Panduan Pendaftaran Box Developer Console:`,
		`<span>📘</span> Panduan Pendaftaran pCloud Developer Console:`,
		`<span>📘</span> Panduan Pendaftaran Yandex OAuth:`,
		`<span>📘</span> Panduan Penghubungan Akun Mega (Native Engine):`,
		`<span>📘</span> Panduan Penghubungan Akun Filen (Zero-Knowledge E2EE):`,
		`<span>📘</span> Panduan Kredensial Backblaze B2:`,
		`<span>📘</span> Panduan Autentikasi PikPak:`,
		`<span>📘</span> Panduan Koneksi SFTP / SSH:`,
		`<span>📘</span> Panduan Koneksi SMB / Samba (Windows Share):`,
		`<span>📘</span> Panduan Autentikasi Proton Drive (Zero-Knowledge):`,
		`<span>📘</span> Panduan Koneksi S3 & S3-Compatible Object Storage:`,
		`<span>📘</span> Panduan Koneksi Nextcloud / WebDAV:`,
		`<span>📘</span> Panduan Koneksi Koofr (WebDAV):`,
	}

	for _, snippet := range hardcodedIndonesianSnippets {
		if strings.Contains(content, snippet) {
			t.Errorf("Add Provider guide hardcodes Indonesian snippet: %q", snippet)
		}
	}

	// Must supply English translations for guide headings in Add Provider modal
	if !strings.Contains(content, "Google Drive API registration (Google Auth Platform)") {
		t.Errorf("Add Provider guide missing English translation for the Google Drive registration guide")
	}
}
