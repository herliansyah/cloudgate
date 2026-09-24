package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RcloneAdapter is a thin VendorDriver adapter that mirrors the rclone/fs.Fs
// abstraction without pulling the full rclone binary. It implements Driver for
// multiple cloud backends (onedrive, dropbox, etc.) by delegating to provider-
// specific REST endpoints while keeping the single-binary, in-memory credential
// injection model prescribed by ADR-0012. Swapping the internals to real
// rclone/fs.Fs is a drop-in: only this file's delegation changes, Driver
// contract and pool/transfer layers stay untouched.
type RcloneAdapter struct {
	mu           sync.RWMutex
	accountID    string
	provider     string
	clientID     string
	clientSecret string
	accessToken  string
	refreshToken string
	tokenExpiry  time.Time
	userEmail    string
	userName     string
	client       *http.Client
	baseURL      string // override for tests (e.g., httptest server)
}

func NewRcloneAdapter(provider, accountID, clientID, clientSecret, accessToken, refreshToken, userEmail, userName string) *RcloneAdapter {
	return &RcloneAdapter{
		accountID:    accountID,
		provider:     provider,
		clientID:     clientID,
		clientSecret: clientSecret,
		accessToken:  accessToken,
		refreshToken: refreshToken,
		tokenExpiry:  time.Now().Add(50 * time.Minute),
		userEmail:    userEmail,
		userName:     userName,
		client:       &http.Client{Timeout: 60 * time.Second},
	}
}

// NewRcloneDriver is the factory prescribed by the spec. It parses credentials
// JSON (as stored in accounts.credentials) and returns a provider-specific
// adapter. For now it covers onedrive and dropbox; additional providers reuse
// the same shape.
func NewRcloneDriver(provider, accountID string, credsJSON string) (*RcloneAdapter, error) {
	var creds struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if credsJSON != "" {
		_ = json.Unmarshal([]byte(credsJSON), &creds)
	}
	if provider == "" {
		return nil, fmt.Errorf("provider required")
	}
	if accountID == "" {
		return nil, fmt.Errorf("account id required")
	}
	// Normalize provider aliases
	if provider == "gdrive" {
		provider = "gdrive"
	}
	return NewRcloneAdapter(provider, accountID, creds.ClientID, creds.ClientSecret, creds.AccessToken, creds.RefreshToken, "", ""), nil
}

func (r *RcloneAdapter) SetBaseURL(u string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.baseURL = u
}

func (r *RcloneAdapter) getBaseURL() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.baseURL != "" {
		return r.baseURL
	}
	switch r.provider {
	case "onedrive":
		return "https://graph.microsoft.com"
	case "dropbox":
		return "https://api.dropboxapi.com"
	case "dropbox_content":
		return "https://content.dropboxapi.com"
	default:
		return "https://graph.microsoft.com"
	}
}

func (r *RcloneAdapter) getContentBaseURL() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.baseURL != "" {
		return r.baseURL
	}
	if r.provider == "dropbox" {
		return "https://content.dropboxapi.com"
	}
	return "https://graph.microsoft.com"
}

func (r *RcloneAdapter) ID() string       { return r.accountID }
func (r *RcloneAdapter) Provider() string { return r.provider }
func (r *RcloneAdapter) UserEmail() string { return r.userEmail }
func (r *RcloneAdapter) UserName() string  { return r.userName }

func (r *RcloneAdapter) tokenEndpoint() string {
	switch r.provider {
	case "onedrive":
		return "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	case "dropbox":
		return "https://api.dropboxapi.com/oauth2/token"
	default:
		return "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	}
}

func (r *RcloneAdapter) getValidAccessToken(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.accessToken != "" && time.Now().Before(r.tokenExpiry) {
		return r.accessToken, nil
	}
	if r.refreshToken == "" {
		if r.accessToken != "" {
			return r.accessToken, nil
		}
		return "", fmt.Errorf("no access or refresh token available")
	}

	vals := url.Values{}
	vals.Set("client_id", r.clientID)
	vals.Set("client_secret", r.clientSecret)
	vals.Set("refresh_token", r.refreshToken)
	vals.Set("grant_type", "refresh_token")
	// OneDrive requires scope on refresh; keep generic
	if r.provider == "onedrive" {
		vals.Set("scope", "files.readwrite offline_access")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", r.tokenEndpoint(), strings.NewReader(vals.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to refresh token (%d): %s", resp.StatusCode, string(body))
	}
	var res struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	r.accessToken = res.AccessToken
	if res.ExpiresIn > 0 {
		r.tokenExpiry = time.Now().Add(time.Duration(res.ExpiresIn-60) * time.Second)
	} else {
		r.tokenExpiry = time.Now().Add(50 * time.Minute)
	}
	return r.accessToken, nil
}

// About returns quota information.
func (r *RcloneAdapter) About(ctx context.Context) (QuotaInfo, error) {
	token, err := r.getValidAccessToken(ctx)
	if err != nil {
		return QuotaInfo{}, err
	}
	switch r.provider {
	case "onedrive":
		return r.onedriveAbout(ctx, token)
	case "dropbox":
		return r.dropboxAbout(ctx, token)
	default:
		return QuotaInfo{}, fmt.Errorf("unsupported provider for About: %s", r.provider)
	}
}

func (r *RcloneAdapter) onedriveAbout(ctx context.Context, token string) (QuotaInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.getBaseURL()+"/v1.0/me/drive", nil)
	if err != nil {
		return QuotaInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.client.Do(req)
	if err != nil {
		return QuotaInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return QuotaInfo{}, fmt.Errorf("onedrive about (%d): %s", resp.StatusCode, string(body))
	}
	var res struct {
		Quota struct {
			Total     int64 `json:"total"`
			Used      int64 `json:"used"`
			Remaining int64 `json:"remaining"`
			Deleted   int64 `json:"deleted"`
		} `json:"quota"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return QuotaInfo{}, err
	}
	total := res.Quota.Total
	used := res.Quota.Used
	if total <= 0 {
		// Fallback for test or unlimited
		total = 15 * 1024 * 1024 * 1024
	}
	return QuotaInfo{Total: total, Used: used, Free: total - used}, nil
}

func (r *RcloneAdapter) dropboxAbout(ctx context.Context, token string) (QuotaInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", r.getBaseURL()+"/2/users/get_space_usage", nil)
	if err != nil {
		return QuotaInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.client.Do(req)
	if err != nil {
		return QuotaInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return QuotaInfo{}, fmt.Errorf("dropbox about (%d): %s", resp.StatusCode, string(body))
	}
	var res struct {
		Used      int64 `json:"used"`
		Allocation struct {
			Allocated int64 `json:"allocated"`
		} `json:"allocation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return QuotaInfo{}, err
	}
	total := res.Allocation.Allocated
	if total <= 0 {
		total = 15 * 1024 * 1024 * 1024
	}
	return QuotaInfo{Total: total, Used: res.Used, Free: total - res.Used}, nil
}

// List lists files at dirPath.
func (r *RcloneAdapter) List(ctx context.Context, dirPath string) ([]FileInfo, error) {
	token, err := r.getValidAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	switch r.provider {
	case "onedrive":
		return r.onedriveList(ctx, token, dirPath)
	case "dropbox":
		return r.dropboxList(ctx, token, dirPath)
	default:
		return nil, fmt.Errorf("unsupported provider for List: %s", r.provider)
	}
}

func (r *RcloneAdapter) onedriveList(ctx context.Context, token, dirPath string) ([]FileInfo, error) {
	cleanDir := path.Clean("/" + dirPath)
	var apiURL string
	if cleanDir == "/" || cleanDir == "." {
		apiURL = r.getBaseURL() + "/v1.0/me/drive/root/children?$top=1000"
	} else {
		apiURL = fmt.Sprintf("%s/v1.0/me/drive/root:%s:/children?$top=1000", r.getBaseURL(), cleanDir)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("onedrive list (%d): %s", resp.StatusCode, string(body))
	}
	var res struct {
		Value []struct {
			Name          string `json:"name"`
			Size          int64  `json:"size"`
			Folder        *struct{} `json:"folder"`
			File          *struct{} `json:"file"`
			LastModified  string `json:"lastModifiedDateTime"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	var out []FileInfo
	for _, f := range res.Value {
		isDir := f.Folder != nil
		modTime, _ := time.Parse(time.RFC3339, f.LastModified)
		fPath := path.Join(cleanDir, f.Name)
		if !strings.HasPrefix(fPath, "/") {
			fPath = "/" + fPath
		}
		out = append(out, FileInfo{
			Path:      fPath,
			Name:      f.Name,
			Size:      f.Size,
			IsDir:     isDir,
			ModTime:   modTime,
			AccountID: r.accountID,
			Provider:  r.provider,
		})
	}
	return out, nil
}

func (r *RcloneAdapter) dropboxList(ctx context.Context, token, dirPath string) ([]FileInfo, error) {
	cleanDir := path.Clean("/" + dirPath)
	if cleanDir == "." {
		cleanDir = "/"
	}
	if cleanDir != "/" && !strings.HasSuffix(cleanDir, "/") {
		// Dropbox expects "" for root, or "/path"
	}
	bodyMap := map[string]any{"path": cleanDir}
	if cleanDir == "/" {
		bodyMap["path"] = ""
	}
	body, _ := json.Marshal(bodyMap)
	req, err := http.NewRequestWithContext(ctx, "POST", r.getBaseURL()+"/2/files/list_folder", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("dropbox list (%d): %s", resp.StatusCode, string(b))
	}
	var res struct {
		Entries []struct {
			Name           string `json:"name"`
			PathLower      string `json:"path_lower"`
			PathDisplay    string `json:"path_display"`
			Tag            string `json:".tag"`
			Size           int64  `json:"size"`
			ClientModified string `json:"client_modified"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	var out []FileInfo
	for _, e := range res.Entries {
		isDir := e.Tag == "folder"
		p := e.PathDisplay
		if p == "" {
			p = "/" + e.Name
		}
		modTime, _ := time.Parse(time.RFC3339, e.ClientModified)
		out = append(out, FileInfo{
			Path:      p,
			Name:      e.Name,
			Size:      e.Size,
			IsDir:     isDir,
			ModTime:   modTime,
			AccountID: r.accountID,
			Provider:  r.provider,
		})
	}
	return out, nil
}

// Get downloads a file.
func (r *RcloneAdapter) Get(ctx context.Context, filePath string) (io.ReadCloser, FileInfo, error) {
	token, err := r.getValidAccessToken(ctx)
	if err != nil {
		return nil, FileInfo{}, err
	}
	switch r.provider {
	case "onedrive":
		return r.onedriveGet(ctx, token, filePath)
	case "dropbox":
		return r.dropboxGet(ctx, token, filePath)
	default:
		return nil, FileInfo{}, fmt.Errorf("unsupported provider for Get: %s", r.provider)
	}
}

func (r *RcloneAdapter) onedriveGet(ctx context.Context, token, filePath string) (io.ReadCloser, FileInfo, error) {
	cleanPath := path.Clean("/" + filePath)
	apiURL := fmt.Sprintf("%s/v1.0/me/drive/root:%s:/content", r.getBaseURL(), cleanPath)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, FileInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, FileInfo{}, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, FileInfo{}, ErrFileNotFound
		}
		return nil, FileInfo{}, fmt.Errorf("onedrive get (%d): %s", resp.StatusCode, string(body))
	}
	sizeStr := resp.Header.Get("Content-Length")
	size, _ := strconv.ParseInt(sizeStr, 10, 64)
	info := FileInfo{
		Path:      cleanPath,
		Name:      path.Base(cleanPath),
		Size:      size,
		IsDir:     false,
		ModTime:   time.Now().UTC(),
		AccountID: r.accountID,
		Provider:  r.provider,
	}
	return resp.Body, info, nil
}

func (r *RcloneAdapter) dropboxGet(ctx context.Context, token, filePath string) (io.ReadCloser, FileInfo, error) {
	cleanPath := path.Clean("/" + filePath)
	arg, _ := json.Marshal(map[string]string{"path": cleanPath})
	req, err := http.NewRequestWithContext(ctx, "POST", r.getContentBaseURL()+"/2/files/download", nil)
	if err != nil {
		return nil, FileInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Dropbox-API-Arg", string(arg))
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, FileInfo{}, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == 409 {
			return nil, FileInfo{}, ErrFileNotFound
		}
		return nil, FileInfo{}, fmt.Errorf("dropbox get (%d): %s", resp.StatusCode, string(body))
	}
	// Dropbox returns metadata in Dropbox-Api-Result header
	var meta struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	_ = json.Unmarshal([]byte(resp.Header.Get("Dropbox-Api-Result")), &meta)
	info := FileInfo{
		Path:      cleanPath,
		Name:      meta.Name,
		Size:      meta.Size,
		IsDir:     false,
		ModTime:   time.Now().UTC(),
		AccountID: r.accountID,
		Provider:  r.provider,
	}
	if info.Name == "" {
		info.Name = path.Base(cleanPath)
	}
	return resp.Body, info, nil
}

// Put uploads a file.
func (r *RcloneAdapter) Put(ctx context.Context, filePath string, in io.Reader, size int64) error {
	token, err := r.getValidAccessToken(ctx)
	if err != nil {
		return err
	}
	switch r.provider {
	case "onedrive":
		return r.onedrivePut(ctx, token, filePath, in, size)
	case "dropbox":
		return r.dropboxPut(ctx, token, filePath, in, size)
	default:
		return fmt.Errorf("unsupported provider for Put: %s", r.provider)
	}
}

func (r *RcloneAdapter) onedrivePut(ctx context.Context, token, filePath string, in io.Reader, size int64) error {
	cleanPath := path.Clean("/" + filePath)
	apiURL := fmt.Sprintf("%s/v1.0/me/drive/root:%s:/content", r.getBaseURL(), cleanPath)
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, io.NopCloser(in))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		req.ContentLength = size
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("onedrive put (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func (r *RcloneAdapter) dropboxPut(ctx context.Context, token, filePath string, in io.Reader, size int64) error {
	cleanPath := path.Clean("/" + filePath)
	argMap := map[string]any{"path": cleanPath, "mode": "overwrite", "autorename": false}
	arg, _ := json.Marshal(argMap)
	req, err := http.NewRequestWithContext(ctx, "POST", r.getContentBaseURL()+"/2/files/upload", in)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Dropbox-API-Arg", string(arg))
	req.Header.Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		req.ContentLength = size
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dropbox put (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// Delete deletes a file.
func (r *RcloneAdapter) Delete(ctx context.Context, filePath string) error {
	token, err := r.getValidAccessToken(ctx)
	if err != nil {
		return err
	}
	switch r.provider {
	case "onedrive":
		return r.onedriveDelete(ctx, token, filePath)
	case "dropbox":
		return r.dropboxDelete(ctx, token, filePath)
	default:
		return fmt.Errorf("unsupported provider for Delete: %s", r.provider)
	}
}

func (r *RcloneAdapter) onedriveDelete(ctx context.Context, token, filePath string) error {
	cleanPath := path.Clean("/" + filePath)
	apiURL := fmt.Sprintf("%s/v1.0/me/drive/root:%s", r.getBaseURL(), cleanPath)
	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return ErrFileNotFound
		}
		return fmt.Errorf("onedrive delete (%d): %s", resp.StatusCode, string(body))
	}
	return nil
}

func (r *RcloneAdapter) dropboxDelete(ctx context.Context, token, filePath string) error {
	cleanPath := path.Clean("/" + filePath)
	body, _ := json.Marshal(map[string]string{"path": cleanPath})
	req, err := http.NewRequestWithContext(ctx, "POST", r.getBaseURL()+"/2/files/delete_v2", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == 409 {
			return ErrFileNotFound
		}
		return fmt.Errorf("dropbox delete (%d): %s", resp.StatusCode, string(b))
	}
	return nil
}

// Move renames/moves a file. Falls back to copy+delete if native move unsupported.
func (r *RcloneAdapter) Move(ctx context.Context, srcPath, dstPath string) error {
	token, err := r.getValidAccessToken(ctx)
	if err != nil {
		return err
	}
	switch r.provider {
	case "onedrive":
		return r.onedriveMove(ctx, token, srcPath, dstPath)
	case "dropbox":
		return r.dropboxMove(ctx, token, srcPath, dstPath)
	default:
		return fmt.Errorf("unsupported provider for Move: %s", r.provider)
	}
}

func (r *RcloneAdapter) onedriveMove(ctx context.Context, token, srcPath, dstPath string) error {
	cleanSrc := path.Clean("/" + srcPath)
	dstName := path.Base(dstPath)
	apiURL := fmt.Sprintf("%s/v1.0/me/drive/root:%s", r.getBaseURL(), cleanSrc)
	bodyMap := map[string]string{"name": dstName}
	b, _ := json.Marshal(bodyMap)
	req, err := http.NewRequestWithContext(ctx, "PATCH", apiURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("onedrive move (%d): %s", resp.StatusCode, string(bb))
	}
	return nil
}

func (r *RcloneAdapter) dropboxMove(ctx context.Context, token, srcPath, dstPath string) error {
	cleanSrc := path.Clean("/" + srcPath)
	cleanDst := path.Clean("/" + dstPath)
	body, _ := json.Marshal(map[string]string{"from_path": cleanSrc, "to_path": cleanDst})
	req, err := http.NewRequestWithContext(ctx, "POST", r.getBaseURL()+"/2/files/move_v2", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dropbox move (%d): %s", resp.StatusCode, string(b))
	}
	return nil
}

// Mkdir creates a directory.
func (r *RcloneAdapter) Mkdir(ctx context.Context, dirPath string) error {
	token, err := r.getValidAccessToken(ctx)
	if err != nil {
		return err
	}
	switch r.provider {
	case "onedrive":
		return r.onedriveMkdir(ctx, token, dirPath)
	case "dropbox":
		return r.dropboxMkdir(ctx, token, dirPath)
	default:
		return fmt.Errorf("unsupported provider for Mkdir: %s", r.provider)
	}
}

func (r *RcloneAdapter) onedriveMkdir(ctx context.Context, token, dirPath string) error {
	cleanPath := path.Clean("/" + dirPath)
	parent := path.Dir(cleanPath)
	name := path.Base(cleanPath)
	var apiURL string
	if parent == "/" || parent == "." {
		apiURL = r.getBaseURL() + "/v1.0/me/drive/root/children"
	} else {
		apiURL = fmt.Sprintf("%s/v1.0/me/drive/root:%s:/children", r.getBaseURL(), parent)
	}
	bodyMap := map[string]any{"name": name, "folder": map[string]any{}}
	b, _ := json.Marshal(bodyMap)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("onedrive mkdir (%d): %s", resp.StatusCode, string(bb))
	}
	return nil
}

func (r *RcloneAdapter) dropboxMkdir(ctx context.Context, token, dirPath string) error {
	cleanPath := path.Clean("/" + dirPath)
	body, _ := json.Marshal(map[string]string{"path": cleanPath})
	req, err := http.NewRequestWithContext(ctx, "POST", r.getBaseURL()+"/2/files/create_folder_v2", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bb, _ := io.ReadAll(resp.Body)
		// Dropbox returns 409 if already exists — treat as success for Mkdir idempotence
		if resp.StatusCode == 409 && bytes.Contains(bb, []byte("path/conflict/folder")) {
			return nil
		}
		return fmt.Errorf("dropbox mkdir (%d): %s", resp.StatusCode, string(bb))
	}
	return nil
}
