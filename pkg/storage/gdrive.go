package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GDriveDriver struct {
	mu           sync.RWMutex
	accountID    string
	clientID     string
	clientSecret string
	refreshToken string
	accessToken  string
	tokenExpiry  time.Time
	userEmail    string
	userName     string
	client       *http.Client
	baseURL      string
}

func NewGDriveDriver(accountID, clientID, clientSecret, accessToken, refreshToken string, userEmail, userName string) *GDriveDriver {
	return &GDriveDriver{
		accountID:    accountID,
		clientID:     clientID,
		clientSecret: clientSecret,
		accessToken:  accessToken,
		refreshToken: refreshToken,
		// ponytail: zero expiry forces refresh on first use — stale DB tokens
		// were causing 401s because the old code assumed 50min freshness.
		tokenExpiry:  time.Time{},
		userEmail:    userEmail,
		userName:     userName,
		client:       &http.Client{Timeout: 60 * time.Second},
	}
}

func (g *GDriveDriver) SetBaseURL(url string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.baseURL = url
}

func (g *GDriveDriver) getBaseURL() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.baseURL != "" {
		return g.baseURL
	}
	return "https://www.googleapis.com"
}

func (g *GDriveDriver) ID() string       { return g.accountID }
func (g *GDriveDriver) Provider() string { return "gdrive" }
func (g *GDriveDriver) UserEmail() string { return g.userEmail }
func (g *GDriveDriver) UserName() string  { return g.userName }

func (g *GDriveDriver) getValidAccessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.accessToken != "" && time.Now().Before(g.tokenExpiry) {
		return g.accessToken, nil
	}

	if g.refreshToken == "" {
		if g.accessToken != "" {
			return g.accessToken, nil
		}
		return "", fmt.Errorf("no access or refresh token available")
	}

	// Refresh the token
	vals := url.Values{}
	vals.Set("client_id", g.clientID)
	vals.Set("client_secret", g.clientSecret)
	vals.Set("refresh_token", g.refreshToken)
	vals.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(vals.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.client.Do(req)
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

	g.accessToken = res.AccessToken
	g.tokenExpiry = time.Now().Add(time.Duration(res.ExpiresIn-60) * time.Second)
	return g.accessToken, nil
}

func (g *GDriveDriver) About(ctx context.Context) (QuotaInfo, error) {
	token, err := g.getValidAccessToken(ctx)
	if err != nil {
		return QuotaInfo{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", g.getBaseURL()+"/drive/v3/about?fields=storageQuota,user", nil)
	if err != nil {
		return QuotaInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return QuotaInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return QuotaInfo{}, fmt.Errorf("google drive about API returned status %d", resp.StatusCode)
	}

	var res struct {
		StorageQuota struct {
			Limit string `json:"limit"`
			Usage string `json:"usage"`
		} `json:"storageQuota"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return QuotaInfo{}, err
	}

	total, _ := strconv.ParseInt(res.StorageQuota.Limit, 10, 64)
	used, _ := strconv.ParseInt(res.StorageQuota.Usage, 10, 64)
	if total <= 0 {
		total = 15 * 1024 * 1024 * 1024 // Google Drive default 15GB if unlimited
	}

	return QuotaInfo{
		Total: total,
		Used:  used,
		Free:  total - used,
	}, nil
}

type gdriveFile struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MimeType     string   `json:"mimeType"`
	Size         string   `json:"size"`
	ModifiedTime string   `json:"modifiedTime"`
	Parents      []string `json:"parents"`
}

func (g *GDriveDriver) resolveFolderID(ctx context.Context, token, dirPath string) (string, error) {
	cleanDir := path.Clean("/" + dirPath)
	if cleanDir == "/" || cleanDir == "." || cleanDir == "" {
		return "root", nil
	}

	parts := strings.Split(strings.Trim(cleanDir, "/"), "/")
	currentParentID := "root"

	for _, part := range parts {
		if part == "" {
			continue
		}
		query := fmt.Sprintf("name = '%s' and '%s' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false",
			strings.ReplaceAll(part, "'", "\\'"), currentParentID)
		apiURL := fmt.Sprintf("%s/drive/v3/files?q=%s&fields=files(id,name)&pageSize=1", g.getBaseURL(), url.QueryEscape(query))

		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := g.client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("failed to resolve folder '%s' (%d): %s", part, resp.StatusCode, string(body))
		}

		var res struct {
			Files []struct {
				ID string `json:"id"`
			} `json:"files"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return "", err
		}
		if len(res.Files) == 0 {
			return "", ErrFileNotFound
		}
		currentParentID = res.Files[0].ID
	}

	return currentParentID, nil
}

func (g *GDriveDriver) List(ctx context.Context, dirPath string) ([]FileInfo, error) {
	token, err := g.getValidAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	cleanDir := path.Clean("/" + dirPath)
	if cleanDir == "." {
		cleanDir = "/"
	}

	parentID, err := g.resolveFolderID(ctx, token, cleanDir)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf("'%s' in parents and trashed = false", parentID)
	apiURL := fmt.Sprintf("%s/drive/v3/files?q=%s&fields=files(id,name,mimeType,size,modifiedTime,parents)&pageSize=1000", g.getBaseURL(), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list drive files (%d): %s", resp.StatusCode, string(body))
	}

	var res struct {
		Files []gdriveFile `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	var list []FileInfo
	for _, f := range res.Files {
		isDir := f.MimeType == "application/vnd.google-apps.folder"
		size, _ := strconv.ParseInt(f.Size, 10, 64)
		modTime, _ := time.Parse(time.RFC3339, f.ModifiedTime)

		filePath := path.Join(cleanDir, f.Name)
		if !strings.HasPrefix(filePath, "/") {
			filePath = "/" + filePath
		}

		list = append(list, FileInfo{
			Path:      filePath,
			Name:      f.Name,
			Size:      size,
			IsDir:     isDir,
			ModTime:   modTime,
			AccountID: g.accountID,
			Provider:  "gdrive",
		})
	}

	return list, nil
}

func (g *GDriveDriver) Get(ctx context.Context, filePath string) (io.ReadCloser, FileInfo, error) {
	token, err := g.getValidAccessToken(ctx)
	if err != nil {
		return nil, FileInfo{}, err
	}

	cleanPath := path.Clean("/" + filePath)
	dir := path.Dir(cleanPath)
	fileName := path.Base(cleanPath)

	parentID, err := g.resolveFolderID(ctx, token, dir)
	var query string
	if err == nil && parentID != "" {
		query = fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", strings.ReplaceAll(fileName, "'", "\\'"), parentID)
	} else {
		query = fmt.Sprintf("name = '%s' and trashed = false", strings.ReplaceAll(fileName, "'", "\\'"))
	}
	searchURL := fmt.Sprintf("%s/drive/v3/files?q=%s&fields=files(id,name,size,mimeType)", g.getBaseURL(), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, FileInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, FileInfo{}, err
	}
	defer resp.Body.Close()

	var res struct {
		Files []gdriveFile `json:"files"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Files) == 0 {
		return nil, FileInfo{}, ErrFileNotFound
	}

	fileID := res.Files[0].ID
	size, _ := strconv.ParseInt(res.Files[0].Size, 10, 64)

	// Download media stream
	downURL := fmt.Sprintf("%s/drive/v3/files/%s?alt=media", g.getBaseURL(), fileID)
	downReq, err := http.NewRequestWithContext(ctx, "GET", downURL, nil)
	if err != nil {
		return nil, FileInfo{}, err
	}
	downReq.Header.Set("Authorization", "Bearer "+token)

	downResp, err := g.client.Do(downReq)
	if err != nil {
		return nil, FileInfo{}, err
	}

	if downResp.StatusCode != http.StatusOK {
		downResp.Body.Close()
		return nil, FileInfo{}, fmt.Errorf("failed to download file from Google Drive (%d)", downResp.StatusCode)
	}

	info := FileInfo{
		Path:      cleanPath,
		Name:      fileName,
		Size:      size,
		IsDir:     false,
		ModTime:   time.Now().UTC(),
		AccountID: g.accountID,
		Provider:  "gdrive",
	}

	return downResp.Body, info, nil
}

func (g *GDriveDriver) Put(ctx context.Context, filePath string, in io.Reader, size int64) error {
	token, err := g.getValidAccessToken(ctx)
	if err != nil {
		return err
	}

	cleanPath := path.Clean("/" + filePath)
	dir := path.Dir(cleanPath)
	fileName := path.Base(cleanPath)

	parentID, _ := g.resolveFolderID(ctx, token, dir)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Part 1: Metadata
	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Type", "application/json; charset=UTF-8")
	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return err
	}
	meta := map[string]any{"name": fileName}
	if parentID != "" && parentID != "root" {
		meta["parents"] = []string{parentID}
	}
	_ = json.NewEncoder(metaPart).Encode(meta)

	// Part 2: Media
	mediaHeader := make(textproto.MIMEHeader)
	mediaHeader.Set("Content-Type", "application/octet-stream")
	mediaPart, err := writer.CreatePart(mediaHeader)
	if err != nil {
		return err
	}
	if _, err := io.Copy(mediaPart, in); err != nil {
		return err
	}
	_ = writer.Close()

	uploadURL := g.getBaseURL() + "/upload/drive/v3/files?uploadType=multipart"
	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "multipart/related; boundary="+writer.Boundary())

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload to Google Drive failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (g *GDriveDriver) Delete(ctx context.Context, filePath string) error {
	token, err := g.getValidAccessToken(ctx)
	if err != nil {
		return err
	}

	cleanPath := path.Clean("/" + filePath)
	dir := path.Dir(cleanPath)
	fileName := path.Base(cleanPath)

	parentID, err := g.resolveFolderID(ctx, token, dir)
	var query string
	if err == nil && parentID != "" {
		query = fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", strings.ReplaceAll(fileName, "'", "\\'"), parentID)
	} else {
		query = fmt.Sprintf("name = '%s' and trashed = false", strings.ReplaceAll(fileName, "'", "\\'"))
	}
	searchURL := fmt.Sprintf("%s/drive/v3/files?q=%s&fields=files(id)", g.getBaseURL(), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var res struct {
		Files []gdriveFile `json:"files"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Files) == 0 {
		return ErrFileNotFound
	}

	delURL := fmt.Sprintf("%s/drive/v3/files/%s", g.getBaseURL(), res.Files[0].ID)
	delReq, err := http.NewRequestWithContext(ctx, "DELETE", delURL, nil)
	if err != nil {
		return err
	}
	delReq.Header.Set("Authorization", "Bearer "+token)

	delResp, err := g.client.Do(delReq)
	if err != nil {
		return err
	}
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to delete file on Google Drive (%d)", delResp.StatusCode)
	}

	return nil
}

func (g *GDriveDriver) Move(ctx context.Context, srcPath, dstPath string) error {
	token, err := g.getValidAccessToken(ctx)
	if err != nil {
		return err
	}

	srcClean := path.Clean("/" + srcPath)
	srcDir := path.Dir(srcClean)
	srcName := path.Base(srcClean)
	dstName := path.Base(dstPath)

	parentID, err := g.resolveFolderID(ctx, token, srcDir)
	var query string
	if err == nil && parentID != "" {
		query = fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", strings.ReplaceAll(srcName, "'", "\\'"), parentID)
	} else {
		query = fmt.Sprintf("name = '%s' and trashed = false", strings.ReplaceAll(srcName, "'", "\\'"))
	}
	searchURL := fmt.Sprintf("%s/drive/v3/files?q=%s&fields=files(id)", g.getBaseURL(), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var res struct {
		Files []gdriveFile `json:"files"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if len(res.Files) == 0 {
		return ErrFileNotFound
	}

	// Rename patch
	patchURL := fmt.Sprintf("%s/drive/v3/files/%s", g.getBaseURL(), res.Files[0].ID)
	patchBody, _ := json.Marshal(map[string]string{"name": dstName})
	patchReq, err := http.NewRequestWithContext(ctx, "PATCH", patchURL, bytes.NewReader(patchBody))
	if err != nil {
		return err
	}
	patchReq.Header.Set("Authorization", "Bearer "+token)
	patchReq.Header.Set("Content-Type", "application/json")

	patchResp, err := g.client.Do(patchReq)
	if err != nil {
		return err
	}
	defer patchResp.Body.Close()

	if patchResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to rename file on Google Drive (%d)", patchResp.StatusCode)
	}

	return nil
}

func (g *GDriveDriver) Mkdir(ctx context.Context, dirPath string) error {
	token, err := g.getValidAccessToken(ctx)
	if err != nil {
		return err
	}

	cleanPath := path.Clean("/" + dirPath)
	parentDir := path.Dir(cleanPath)
	folderName := path.Base(cleanPath)

	parentID, _ := g.resolveFolderID(ctx, token, parentDir)
	meta := map[string]any{
		"name":     folderName,
		"mimeType": "application/vnd.google-apps.folder",
	}
	if parentID != "" && parentID != "root" {
		meta["parents"] = []string{parentID}
	}

	body, _ := json.Marshal(meta)
	req, err := http.NewRequestWithContext(ctx, "POST", g.getBaseURL()+"/drive/v3/files", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("failed to create directory on Google Drive (%d)", resp.StatusCode)
	}

	return nil
}
