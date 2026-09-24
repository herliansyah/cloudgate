package sync

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/herliansyah/cloudgate/pkg/vault"
)

type GitHubSyncClient struct {
	token string
	owner string
	repo  string
	path  string
}

func NewGitHubSyncClient(token, repoSlug, filePath string) (*GitHubSyncClient, error) {
	parts := strings.Split(repoSlug, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository format, expected 'owner/repo'")
	}
	if filePath == "" {
		filePath = "cloudgate-vault.enc"
	}

	return &GitHubSyncClient{
		token: token,
		owner: parts[0],
		repo:  parts[1],
		path:  filePath,
	}, nil
}

type gitHubContentResponse struct {
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

type gitHubPutRequest struct {
	Message string `json:"message"`
	Content string `json:"content"`
	SHA     string `json:"sha,omitempty"`
}

// PushVault encrypts the plaintext payload and uploads it as a commit to GitHub.
func (g *GitHubSyncClient) PushVault(ctx context.Context, plaintext []byte, passphrase string) error {
	encrypted, err := vault.Encrypt(plaintext, passphrase)
	if err != nil {
		return fmt.Errorf("failed to encrypt vault before push: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(encrypted)
	existingSHA, _ := g.getFileSHA(ctx)

	body := gitHubPutRequest{
		Message: "Backup Cloudgate encrypted vault: " + time.Now().UTC().Format(time.RFC3339),
		Content: encoded,
		SHA:     existingSHA,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", g.owner, g.repo, g.path)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "token "+g.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Cloudgate-Sync")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub push failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API push returned error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// PullVault fetches the encrypted file from GitHub and decrypts it with the passphrase.
func (g *GitHubSyncClient) PullVault(ctx context.Context, passphrase string) ([]byte, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", g.owner, g.repo, g.path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "token "+g.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Cloudgate-Sync")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub pull failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub pull returned status %d", resp.StatusCode)
	}

	var res gitHubContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	cleanBase64 := strings.ReplaceAll(res.Content, "\n", "")
	encrypted, err := base64.StdEncoding.DecodeString(cleanBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 content from GitHub: %w", err)
	}

	return vault.Decrypt(encrypted, passphrase)
}

func (g *GitHubSyncClient) getFileSHA(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", g.owner, g.repo, g.path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+g.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("file not found")
	}

	var res gitHubContentResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.SHA, nil
}
