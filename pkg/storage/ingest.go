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

// isBlockedIP checks if an IP is loopback, private, link-local, unspecified, or cloud metadata.
func isBlockedIP(ip net.IP) bool {
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
	if hLower == "localhost" || strings.HasSuffix(hLower, ".localhost") || strings.HasSuffix(hLower, ".local") {
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
		Timeout:   0, // No global timeout for long streaming downloads; context handles cancellation
	}
}

// ResolveIngestFilename resolves the destination filename using the 4-tier hierarchy.
func ResolveIngestFilename(resp *http.Response, parsedURL *url.URL, customName string) string {
	if customName != "" {
		clean := filepath.Base(filepath.Clean(customName))
		if clean != "." && clean != "/" && clean != "" {
			return clean
		}
	}

	if resp != nil {
		cd := resp.Header.Get("Content-Disposition")
		if cd != "" {
			if _, params, err := mime.ParseMediaType(cd); err == nil {
				if fn, ok := params["filename"]; ok && fn != "" {
					clean := filepath.Base(filepath.Clean(fn))
					if clean != "." && clean != "/" && clean != "" {
						return clean
					}
				}
			}
		}
	}

	if parsedURL != nil && parsedURL.Path != "" {
		base := path.Base(parsedURL.Path)
		if base != "." && base != "/" && base != "" {
			// ponytail: Strip query-like trailing artifacts if any
			if idx := strings.IndexAny(base, "?#"); idx != -1 {
				base = base[:idx]
			}
			if base != "" {
				return base
			}
		}
	}

	return fmt.Sprintf("download-%d", time.Now().Unix())
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

	if onProgress != nil {
		onProgress(reader.readBytes, reader.readBytes)
	}

	return targetFilePath, reader.readBytes, nil
}
