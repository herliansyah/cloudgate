package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/oauth2"
)

// ── TOTP (RFC 6238) ──

// totpCode computes the current 6-digit TOTP code (SHA1, 30s step) for a
// base32 secret, as used by MEGA and Proton authenticator apps.
func totpCode(secret string, now time.Time) (string, error) {
	s := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(secret)))
	s = strings.TrimRight(s, "=")
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("secret must be base32: %w", err)
	}
	if len(key) == 0 {
		return "", errors.New("secret is empty")
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", code), nil
}

// ── SFTP host key pinning (trust on first use) ──

var (
	knownHostsDirOnce sync.Once
	knownHostsDir     string
	knownHostsDirErr  error
)

func runtimeKnownHostsDir() (string, error) {
	knownHostsDirOnce.Do(func() {
		knownHostsDir, knownHostsDirErr = os.MkdirTemp("", "cloudgate-known-hosts-")
	})
	return knownHostsDir, knownHostsDirErr
}

// sshHostKeyFetcher is replaceable in tests.
var sshHostKeyFetcher = fetchSSHHostKey

func fetchSSHHostKey(ctx context.Context, addr string) (ssh.PublicKey, error) {
	dialer := net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	var captured ssh.PublicKey
	errCaptured := errors.New("host key captured")
	cfg := &ssh.ClientConfig{
		User: "cloudgate-hostkey-probe",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured = key
			return errCaptured
		},
		Timeout: 15 * time.Second,
	}
	_, _, _, err = ssh.NewClientConn(conn, addr, cfg)
	if captured != nil {
		return captured, nil
	}
	if err == nil {
		err = errors.New("no host key presented")
	}
	return nil, err
}

// ensureSFTPHostKey pins the server host key on first connection (stored in
// credentials as known_host_key) and hands rclone a known_hosts file so every
// later connection verifies the key instead of trusting any server (MITM).
func (r *RcloneAdapter) ensureSFTPHostKey(ctx context.Context) error {
	creds := r.Credentials()
	host := first(creds, "host")
	if host == "" {
		return errors.New("SFTP: host is required")
	}
	port := first(creds, "port")
	if port == "" {
		port = "22"
	}
	addr := net.JoinHostPort(host, port)
	line := first(creds, "known_host_key")
	if line == "" {
		key, err := sshHostKeyFetcher(ctx, addr)
		if err != nil {
			return fmt.Errorf("SFTP: cannot read host key from %s: %w", addr, err)
		}
		line = knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)
		r.updateCreds(func(c map[string]string) {
			c["known_host_key"] = line
			c["host_key_fingerprint"] = ssh.FingerprintSHA256(key)
		})
	}
	dir, err := runtimeKnownHostsDir()
	if err != nil {
		return fmt.Errorf("SFTP: cannot create known_hosts directory: %w", err)
	}
	sum := sha256.Sum256([]byte(r.accountID))
	file := filepath.Join(dir, hex.EncodeToString(sum[:8])+".known_hosts")
	if err := os.WriteFile(file, []byte(line+"\n"), 0o600); err != nil {
		return fmt.Errorf("SFTP: cannot write known_hosts: %w", err)
	}
	r.mu.Lock()
	r.knownHostsFile = file
	r.mu.Unlock()
	return nil
}

// HostKeyFingerprint returns the pinned SFTP host key fingerprint, if any.
func (r *RcloneAdapter) HostKeyFingerprint() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.creds["host_key_fingerprint"]
}

// ── OneDrive drive discovery ──

// Replaceable in tests.
var (
	oneDriveTokenURL = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	msGraphBaseURL   = "https://graph.microsoft.com"
)

// ensureOneDriveDrive discovers drive_id/drive_type (required by rclone's
// onedrive backend) the same way `rclone config` does, and persists them.
func (r *RcloneAdapter) ensureOneDriveDrive(ctx context.Context) error {
	creds := r.Credentials()
	if first(creds, "drive_id") != "" && first(creds, "drive_type") != "" {
		return nil
	}
	tok := tokenFromCreds(creds)
	if tok == nil {
		return errors.New("OneDrive: missing OAuth token - reconnect the account")
	}
	conf := &oauth2.Config{
		ClientID:     first(creds, "client_id"),
		ClientSecret: firstRaw(creds, "client_secret"),
		Endpoint:     oauth2.Endpoint{TokenURL: oneDriveTokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
	fresh, err := conf.TokenSource(ctx, tok).Token()
	if err != nil {
		return fmt.Errorf("OneDrive: token refresh failed: %w", err)
	}
	if fresh.AccessToken != tok.AccessToken || fresh.RefreshToken != tok.RefreshToken {
		js := tokenJSON(fresh)
		r.updateCreds(func(c map[string]string) { storeBackendState(c, "token", js) })
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, msGraphBaseURL+"/v1.0/me/drive", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+fresh.AccessToken)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("OneDrive: drive lookup failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OneDrive: drive lookup failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var drive struct {
		ID        string `json:"id"`
		DriveType string `json:"driveType"`
	}
	if err := json.Unmarshal(body, &drive); err != nil || drive.ID == "" || drive.DriveType == "" {
		return fmt.Errorf("OneDrive: unexpected drive response: %s", strings.TrimSpace(string(body)))
	}
	r.updateCreds(func(c map[string]string) {
		c["drive_id"] = drive.ID
		c["drive_type"] = drive.DriveType
	})
	return nil
}
