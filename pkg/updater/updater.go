package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/herliansyah/cloudgate/pkg/config"
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
	Changelog       string `json:"changelog,omitempty"`
	ReleaseURL      string `json:"release_url,omitempty"`
}

// CheckUpdate checks GitHub Releases API for new versions of Cloudgate.
func CheckUpdate(ctx context.Context) (*UpdateCheckResult, error) {
	apiURL := "https://api.github.com/repos/herliansyah/cloudgate/releases/latest"

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Cloudgate-Updater/"+config.AppVersion)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query GitHub releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// No release published yet
		return &UpdateCheckResult{
			CurrentVersion:  config.AppVersion,
			LatestVersion:   config.AppVersion,
			UpdateAvailable: false,
		}, nil
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

	updateAvailable := cleanLatest != cleanCurrent && cleanLatest > cleanCurrent

	// Locate binary asset for current OS/Arch
	expectedPattern := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, asset := range rel.Assets {
		if strings.Contains(strings.ToLower(asset.Name), expectedPattern) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	return &UpdateCheckResult{
		CurrentVersion:  config.AppVersion,
		LatestVersion:   rel.TagName,
		UpdateAvailable: updateAvailable,
		DownloadURL:     downloadURL,
		Changelog:       rel.Body,
		ReleaseURL:      rel.HTMLURL,
	}, nil
}

// ApplyUpdate downloads the binary and atomically replaces the currently executing executable.
func ApplyUpdate(ctx context.Context, downloadURL string) error {
	if downloadURL == "" {
		return fmt.Errorf("download URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Cloudgate-Updater/"+config.AppVersion)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download release binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	tempNewFile := executablePath + ".new"
	out, err := os.OpenFile(tempNewFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temporary update file: %w", err)
	}

	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tempNewFile)
		return fmt.Errorf("failed to save update payload: %w", err)
	}
	out.Close()

	// Atomically replace executable
	if err := os.Rename(tempNewFile, executablePath); err != nil {
		_ = os.Remove(tempNewFile)
		return fmt.Errorf("failed to replace executable: %w", err)
	}

	return nil
}
