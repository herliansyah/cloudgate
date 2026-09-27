package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herliansyah/cloudgate/pkg/config"
)

//go:embed CHANGELOG.md
var embeddedChangelog string

var (
	cacheMu       sync.RWMutex
	cachedResult  *UpdateCheckResult
	lastCheckTime time.Time
	cacheTTL      = 24 * time.Hour
)

type ReleaseInfo struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []Asset   `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type UpdateCheckResult struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	DownloadURL     string `json:"download_url,omitempty"`
	ChecksumURL     string `json:"checksum_url,omitempty"`
	Changelog       string `json:"changelog,omitempty"`
	ReleaseURL      string `json:"release_url,omitempty"`
}

// GetChangelog returns the human-readable ReleaseChangelog.
// It prefers the live CHANGELOG.md from disk if running from a local checkout,
// falling back to the embedded changelog within the executable.
func GetChangelog() string {
	if data, err := os.ReadFile("CHANGELOG.md"); err == nil && len(data) > 0 {
		return string(data)
	}
	return embeddedChangelog
}

// CheckUpdate checks the GitHub Releases API for new versions of Cloudgate.
// When force is false, it returns in-memory cached results if checked within the last 24 hours.
func CheckUpdate(ctx context.Context, force bool) (*UpdateCheckResult, error) {
	if !force {
		cacheMu.RLock()
		if cachedResult != nil && time.Since(lastCheckTime) < cacheTTL {
			res := *cachedResult
			cacheMu.RUnlock()
			return &res, nil
		}
		cacheMu.RUnlock()
	}

	apiURL := "https://api.github.com/repos/herliansyah/cloudgate/releases/latest"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Cloudgate-Updater/"+config.AppVersion)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		result := &UpdateCheckResult{
			CurrentVersion:  config.AppVersion,
			LatestVersion:   config.AppVersion,
			UpdateAvailable: false,
		}
		cacheResult(result)
		return result, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var rel ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to decode release payload: %w", err)
	}

	cleanLatest := strings.TrimPrefix(rel.TagName, "v")
	cleanCurrent := strings.TrimPrefix(config.AppVersion, "v")
	updateAvailable := isNewerVersion(cleanLatest, cleanCurrent)

	// Locate binary asset and checksum asset
	expectedPattern1 := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	expectedPattern2 := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	var checksumURL string

	for _, asset := range rel.Assets {
		nameLower := strings.ToLower(asset.Name)
		if strings.Contains(nameLower, "checksum") || strings.HasSuffix(nameLower, ".sha256") {
			checksumURL = asset.BrowserDownloadURL
		}
		if strings.Contains(nameLower, expectedPattern1) || strings.Contains(nameLower, expectedPattern2) {
			downloadURL = asset.BrowserDownloadURL
		}
	}

	result := &UpdateCheckResult{
		CurrentVersion:  config.AppVersion,
		LatestVersion:   rel.TagName,
		UpdateAvailable: updateAvailable,
		DownloadURL:     downloadURL,
		ChecksumURL:     checksumURL,
		Changelog:       rel.Body,
		ReleaseURL:      rel.HTMLURL,
	}

	cacheResult(result)
	return result, nil
}

func cacheResult(res *UpdateCheckResult) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	copied := *res
	cachedResult = &copied
	lastCheckTime = time.Now()
}

// isNewerVersion compares semver version strings (e.g. "0.2.0" vs "0.1.0", "0.10.0" vs "0.9.0").
func isNewerVersion(latest, current string) bool {
	if latest == "" || latest == current {
		return false
	}

	lParts := strings.Split(latest, ".")
	cParts := strings.Split(current, ".")

	maxLen := len(lParts)
	if len(cParts) > maxLen {
		maxLen = len(cParts)
	}

	for i := 0; i < maxLen; i++ {
		var lVal, cVal int
		if i < len(lParts) {
			lVal, _ = strconv.Atoi(strings.Split(lParts[i], "-")[0])
		}
		if i < len(cParts) {
			cVal, _ = strconv.Atoi(strings.Split(cParts[i], "-")[0])
		}

		if lVal > cVal {
			return true
		}
		if lVal < cVal {
			return false
		}
	}
	return false
}

// ApplyUpdate downloads the ReleasePackage binary, strictly validates its SHA-256 checksum
// against checksums.txt, and atomically replaces the currently executing binary.
func ApplyUpdate(ctx context.Context, downloadURL, checksumURL string) error {
	if downloadURL == "" {
		return fmt.Errorf("download URL is empty")
	}
	if checksumURL == "" {
		return fmt.Errorf("checksum asset (checksums.txt) not found in release: strict verification required")
	}

	client := &http.Client{Timeout: 90 * time.Second}

	// 1. Download and parse checksums
	reqChk, err := http.NewRequestWithContext(ctx, "GET", checksumURL, nil)
	if err != nil {
		return fmt.Errorf("failed to prepare checksum request: %w", err)
	}
	reqChk.Header.Set("User-Agent", "Cloudgate-Updater/"+config.AppVersion)

	respChk, err := client.Do(reqChk)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}
	defer respChk.Body.Close()

	if respChk.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch checksums, status %d", respChk.StatusCode)
	}

	checksumMap := parseChecksums(respChk.Body)
	if len(checksumMap) == 0 {
		return fmt.Errorf("checksum file is empty or format unrecognized")
	}

	// 2. Download binary payload to temporary file and compute SHA-256 hash simultaneously
	reqBin, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to prepare binary download request: %w", err)
	}
	reqBin.Header.Set("User-Agent", "Cloudgate-Updater/"+config.AppVersion)

	respBin, err := client.Do(reqBin)
	if err != nil {
		return fmt.Errorf("failed to download release binary: %w", err)
	}
	defer respBin.Body.Close()

	if respBin.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", respBin.StatusCode)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}
	// Resolve symlinks to target real binary file
	if realPath, err := filepath.EvalSymlinks(executablePath); err == nil {
		executablePath = realPath
	}

	tempNewFile := executablePath + ".new"
	out, err := os.OpenFile(tempNewFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temporary update file: %w", err)
	}

	hasher := sha256.New()
	multiWriter := io.MultiWriter(out, hasher)

	if _, err := io.Copy(multiWriter, respBin.Body); err != nil {
		out.Close()
		_ = os.Remove(tempNewFile)
		return fmt.Errorf("failed to save update payload: %w", err)
	}
	out.Close()

	calculatedHash := hex.EncodeToString(hasher.Sum(nil))

	// 3. Find expected checksum for this asset
	assetFilename := extractFilename(downloadURL)
	expectedHash := findExpectedChecksum(checksumMap, assetFilename)
	if expectedHash == "" {
		_ = os.Remove(tempNewFile)
		return fmt.Errorf("no matching checksum found for asset %q in checksums file", assetFilename)
	}

	if !strings.EqualFold(calculatedHash, expectedHash) {
		_ = os.Remove(tempNewFile)
		return fmt.Errorf("SHA-256 verification failed for %s: expected %s, got %s", assetFilename, expectedHash, calculatedHash)
	}

	// 4. Ensure executable permissions
	_ = os.Chmod(tempNewFile, 0755)

	// 5. Atomically replace executable
	if err := replaceBinary(tempNewFile, executablePath); err != nil {
		_ = os.Remove(tempNewFile)
		return fmt.Errorf("failed to replace executable: %w", err)
	}

	return nil
}

// RestartProcess triggers the ProcessRestart lifecycle: re-executing the updated binary.
func RestartProcess() error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}
	if realPath, err := filepath.EvalSymlinks(executablePath); err == nil {
		executablePath = realPath
	}
	return execRestart(executablePath, os.Args, os.Environ())
}

// parseChecksums parses a standard sha256sum file (<hash>  <filename>).
func parseChecksums(r io.Reader) map[string]string {
	res := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hash := strings.ToLower(parts[0])
			filename := filepath.Base(strings.TrimPrefix(parts[1], "*"))
			res[filename] = hash
		}
	}
	return res
}

func extractFilename(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return filepath.Base(rawURL)
	}
	return filepath.Base(u.Path)
}

func findExpectedChecksum(checksumMap map[string]string, assetFilename string) string {
	// Exact match
	if hash, ok := checksumMap[assetFilename]; ok {
		return hash
	}
	// Case-insensitive match
	for name, hash := range checksumMap {
		if strings.EqualFold(name, assetFilename) {
			return hash
		}
	}
	// Suffix / substring match
	for name, hash := range checksumMap {
		if strings.HasSuffix(assetFilename, name) || strings.HasSuffix(name, assetFilename) {
			return hash
		}
	}
	return ""
}

// replaceBinary replaces target with source, handling Windows file locking.
func replaceBinary(source, target string) error {
	if runtime.GOOS == "windows" {
		oldBackup := target + ".old"
		_ = os.Remove(oldBackup)
		if err := os.Rename(target, oldBackup); err != nil {
			return err
		}
		return os.Rename(source, target)
	}
	return os.Rename(source, target)
}
