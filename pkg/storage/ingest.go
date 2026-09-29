package storage

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// AllowLocalForTesting allows testing against local httptest servers when set to true.
var AllowLocalForTesting = false

// isBlockedIP checks if an IP is loopback, private, link-local, unspecified, or cloud metadata.
func isBlockedIP(ip net.IP) bool {
	if AllowLocalForTesting {
		return false
	}
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Cloud metadata service 169.254.169.254
	if ip.String() == "169.254.169.254" {
		return true
	}
	return false
}

// ValidateSSRF checks if the given URL is safe to fetch (HTTP/HTTPS only and no private/internal IPs).
func ValidateSSRF(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported protocol scheme: %s (only http and https allowed)", scheme)
	}

	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL missing host")
	}

	hLower := strings.ToLower(host)
	if !AllowLocalForTesting && (hLower == "localhost" || strings.HasSuffix(hLower, ".localhost") || strings.HasSuffix(hLower, ".local")) {
		return nil, fmt.Errorf("connection to localhost or local domain is prohibited")
	}

	// Immediate static check if host is an IP literal
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("connection to restricted IP %s is prohibited", host)
		}
	} else {
		// Resolve hostname and verify no resolved IP is restricted
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err == nil {
			for _, ip := range ips {
				if isBlockedIP(ip) {
					return nil, fmt.Errorf("host %s resolves to restricted IP %s", host, ip.String())
				}
			}
		}
	}

	return parsed, nil
}

// newSSRFSafeClient returns an http.Client whose transport prevents DNS rebinding to internal IPs.
func newSSRFSafeClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 15 * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
			}
			var safeIP net.IP
			for _, ip := range ips {
				if !isBlockedIP(ip) {
					safeIP = ip
					break
				}
			}
			if safeIP == nil {
				return nil, fmt.Errorf("all resolved IP addresses for %s are private or restricted", host)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(safeIP.String(), port))
		},
		ResponseHeaderTimeout: 30 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if _, err := ValidateSSRF(req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			return nil
		},
		Timeout: 0, // No global timeout for long streaming downloads; context handles cancellation
	}
}

// ResolveIngestFilename resolves the destination filename using the 5-tier hierarchy.
func ResolveIngestFilename(resp *http.Response, parsedURL *url.URL, customName string) string {
	// 1. Explicit user custom name
	if customName != "" {
		clean := filepath.Base(filepath.Clean(customName))
		if clean != "." && clean != "/" && clean != "" {
			return clean
		}
	}

	// Prefer effective redirected URL when available
	effectiveURL := parsedURL
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
		effectiveURL = resp.Request.URL
	}

	// 2. Content-Disposition header (RFC 6266 / RFC 5987)
	if resp != nil {
		cd := resp.Header.Get("Content-Disposition")
		if cd != "" {
			if _, params, err := mime.ParseMediaType(cd); err == nil {
				// 2a. RFC 5987 filename* (e.g. UTF-8''my%20report.pdf)
				if fnStar, ok := params["filename*"]; ok && fnStar != "" {
					val := fnStar
					if parts := strings.SplitN(fnStar, "''", 2); len(parts) == 2 {
						val = parts[1]
					}
					if unescaped, err := url.QueryUnescape(val); err == nil && unescaped != "" {
						clean := filepath.Base(filepath.Clean(unescaped))
						if clean != "." && clean != "/" && clean != "" {
							return clean
						}
					}
				}
				// 2b. Standard filename
				if fn, ok := params["filename"]; ok && fn != "" {
					if unescaped, err := url.QueryUnescape(fn); err == nil && strings.Contains(fn, "%") {
						fn = unescaped
					}
					clean := filepath.Base(filepath.Clean(fn))
					if clean != "." && clean != "/" && clean != "" {
						return clean
					}
				}
			}
		}
	}

	// 3. URL path base
	var candidateBase string
	if effectiveURL != nil && effectiveURL.Path != "" {
		base := path.Base(effectiveURL.Path)
		if base != "." && base != "/" && base != "" {
			if idx := strings.IndexAny(base, "?#"); idx != -1 {
				base = base[:idx]
			}
			if unescaped, err := url.PathUnescape(base); err == nil && unescaped != "" {
				base = unescaped
			}
			if base != "." && base != "/" && base != "" {
				candidateBase = base
			}
		}
	}

	// If candidateBase has an explicit extension and is not a generic stub, use it
	if candidateBase != "" && candidateBase != "download" && candidateBase != "file" && path.Ext(candidateBase) != "" {
		return candidateBase
	}

	// 4. Content-Type extension deduction
	var mimeExt string
	if resp != nil {
		ct := resp.Header.Get("Content-Type")
		if ct != "" {
			if mediaType, _, err := mime.ParseMediaType(ct); err == nil && mediaType != "application/octet-stream" {
				if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
					mimeExt = exts[0]
					for _, e := range exts {
						switch e {
						case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf", ".zip", ".tar", ".gz", ".csv", ".json", ".txt", ".mp4", ".mp3":
							mimeExt = e
						}
					}
				}
			}
		}
	}

	if candidateBase != "" && candidateBase != "download" && candidateBase != "file" {
		if path.Ext(candidateBase) == "" && mimeExt != "" {
			return candidateBase + mimeExt
		}
		return candidateBase
	}

	// 5. Fallback timestamped filename
	return fmt.Sprintf("download-%d%s", time.Now().Unix(), mimeExt)
}

// countingReader wraps an io.Reader and periodically reports progress.
type countingReader struct {
	reader     io.Reader
	totalBytes int64
	readBytes  int64
	onProgress func(readBytes, totalBytes int64)
	lastReport time.Time
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.reader.Read(p)
	if n > 0 {
		cr.readBytes += int64(n)
		if cr.onProgress != nil && time.Since(cr.lastReport) > 500*time.Millisecond {
			cr.onProgress(cr.readBytes, cr.totalBytes)
			cr.lastReport = time.Now()
		}
	}
	return n, err
}

// RemoteIngestStream downloads from rawURL and streams directly into dstDriver.Put.
func RemoteIngestStream(
	ctx context.Context,
	rawURL string,
	customName string,
	dstDriver Driver,
	dstDir string,
	onProgress func(readBytes, totalBytes int64),
) (string, int64, error) {
	parsedURL, err := ValidateSSRF(rawURL)
	if err != nil {
		return "", 0, fmt.Errorf("SSRF validation failed: %w", err)
	}

	client := newSSRFSafeClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Cloudgate-Ingest/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to connect to remote URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("remote server returned HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	finalFilename := ResolveIngestFilename(resp, parsedURL, customName)
	targetFilePath := path.Join(dstDir, finalFilename)

	// Ensure parent destination directory exists before upload
	targetDir := path.Dir(targetFilePath)
	if targetDir != "" && targetDir != "." && targetDir != "/" {
		_ = dstDriver.Mkdir(ctx, targetDir)
	}

	contentLength := resp.ContentLength
	if contentLength < 0 {
		contentLength = -1 // Unknown length for chunked transfers
	}

	reader := &countingReader{
		reader:     resp.Body,
		totalBytes: contentLength,
		onProgress: onProgress,
		lastReport: time.Now(),
	}

	if err := dstDriver.Put(ctx, targetFilePath, reader, contentLength); err != nil {
		return "", 0, fmt.Errorf("failed to store ingested file into remote account: %w", err)
	}

	// Verify complete transfer when Content-Length was advertised
	if contentLength > 0 && reader.readBytes < contentLength {
		return "", reader.readBytes, fmt.Errorf("download incomplete: received %d of %d expected bytes", reader.readBytes, contentLength)
	}

	if onProgress != nil {
		onProgress(reader.readBytes, reader.readBytes)
	}

	return targetFilePath, reader.readBytes, nil
}
