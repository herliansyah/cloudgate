package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/object"
	"github.com/rclone/rclone/fs/operations"
	"golang.org/x/oauth2"

	// Embedded rclone backends (ADR-0012 / ADR-0027). Importing a backend
	// registers it in rclone's fs registry; RcloneAdapter looks them up by name.
	_ "github.com/rclone/rclone/backend/b2"
	_ "github.com/rclone/rclone/backend/box"
	_ "github.com/rclone/rclone/backend/drive"
	_ "github.com/rclone/rclone/backend/dropbox"
	_ "github.com/rclone/rclone/backend/filen"
	_ "github.com/rclone/rclone/backend/koofr"
	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/mega"
	_ "github.com/rclone/rclone/backend/memory"
	_ "github.com/rclone/rclone/backend/onedrive"
	_ "github.com/rclone/rclone/backend/pcloud"
	_ "github.com/rclone/rclone/backend/pikpak"
	_ "github.com/rclone/rclone/backend/protondrive"
	_ "github.com/rclone/rclone/backend/s3"
	_ "github.com/rclone/rclone/backend/sftp"
	_ "github.com/rclone/rclone/backend/smb"
	_ "github.com/rclone/rclone/backend/webdav"
	_ "github.com/rclone/rclone/backend/yandex"
)

// Provider is a typed cloud provider identifier.
type Provider string

const (
	ProviderGDrive      Provider = "gdrive"
	ProviderOneDrive    Provider = "onedrive"
	ProviderDropbox     Provider = "dropbox"
	ProviderBox         Provider = "box"
	ProviderPCloud      Provider = "pcloud"
	ProviderYandex      Provider = "yandex"
	ProviderKoofr       Provider = "koofr"
	ProviderS3          Provider = "s3"
	ProviderWebDAV      Provider = "webdav"
	ProviderMega        Provider = "mega"
	ProviderFilen       Provider = "filen"
	ProviderB2          Provider = "b2"
	ProviderPikPak      Provider = "pikpak"
	ProviderSFTP        Provider = "sftp"
	ProviderSMB         Provider = "smb"
	ProviderProtonDrive Provider = "protondrive"
)

// SupportedProviders lists every provider CloudGate can connect to.
var SupportedProviders = []Provider{
	ProviderGDrive, ProviderOneDrive, ProviderDropbox, ProviderBox, ProviderPCloud, ProviderYandex,
	ProviderKoofr, ProviderS3, ProviderWebDAV, ProviderMega, ProviderFilen, ProviderB2,
	ProviderPikPak, ProviderSFTP, ProviderSMB, ProviderProtonDrive,
}

// NormalizeProvider maps aliases (e.g. "google") onto canonical provider IDs.
func NormalizeProvider(p string) Provider {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "google" {
		return ProviderGDrive
	}
	return Provider(p)
}

// IsSupportedProvider reports whether p (or its alias) is a supported provider.
func IsSupportedProvider(p string) bool {
	np := NormalizeProvider(p)
	for _, sp := range SupportedProviders {
		if sp == np {
			return true
		}
	}
	return false
}

// IsOAuthProvider reports whether the provider is connected through the OAuth flow.
func IsOAuthProvider(p Provider) bool {
	switch p {
	case ProviderGDrive, ProviderOneDrive, ProviderDropbox, ProviderBox, ProviderPCloud, ProviderYandex:
		return true
	}
	return false
}

// unmeteredQuota is reported for backends that have no quota concept
// (S3, B2, WebDAV/SFTP servers without quota support). 1 TiB keeps the
// capacity-aware pool usable without pretending to know the real limit.
const unmeteredQuota = int64(1) << 40

// UnmeteredQuota is the synthetic total reported by backends without a quota API.
const UnmeteredQuota = unmeteredQuota

// IsUnmetered reports whether q is the synthetic quota of an unmetered backend.
func IsUnmetered(q QuotaInfo) bool { return q.Total == unmeteredQuota }

func syntheticQuota(used int64) QuotaInfo {
	if used < 0 {
		used = 0
	}
	total := unmeteredQuota
	if used > total {
		total = used
	}
	return QuotaInfo{Total: total, Used: used, Free: total - used}
}

// CredentialPersister is invoked whenever the backend rotates or learns new
// credential state (refreshed OAuth tokens, Proton/PikPak sessions, OneDrive
// drive IDs, SFTP host keys). Implementations must persist creds for accountID.
type CredentialPersister func(accountID string, creds map[string]string)

// AdapterOption customises an RcloneAdapter.
type AdapterOption func(*RcloneAdapter)

// WithMemoryBackend makes the adapter use rclone's in-process memory backend
// instead of the real provider. Intended ONLY for tests; it is never enabled
// from user-supplied data.
func WithMemoryBackend() AdapterOption {
	return func(a *RcloneAdapter) { a.memory = true }
}

// WithPersister registers the callback used to save rotated credentials.
func WithPersister(p CredentialPersister) AdapterOption {
	return func(a *RcloneAdapter) { a.persister = p }
}

// WithIdentity sets the display identity (principal) of the account.
func WithIdentity(email, name string) AdapterOption {
	return func(a *RcloneAdapter) {
		a.userEmail = email
		a.userName = name
	}
}

const (
	initRetryBackoff = 20 * time.Second
	quotaCacheTTL    = 30 * time.Second
)

// RcloneAdapter implements Driver on top of an embedded rclone fs.Fs for every
// supported provider. Provider-specific concerns (endpoints, pagination,
// chunked uploads, token refresh, encoding) are delegated to rclone.
type RcloneAdapter struct {
	accountID string
	provider  Provider
	memory    bool
	persister CredentialPersister

	mu        sync.RWMutex // guards creds, cfg, identity
	creds     map[string]string
	userEmail string
	userName  string

	knownHostsFile string     // runtime known_hosts file for SFTP host key pinning
	initMu         sync.Mutex // guards f, init state
	f              fs.Fs
	initDone       chan struct{}
	lastInitErr    error
	lastInitAt     time.Time

	quotaMu  sync.Mutex
	quota    QuotaInfo
	quotaErr error
	quotaAt  time.Time
}

// NewRcloneDriverFromCreds builds an adapter from a credential map as stored in
// accounts.credentials. The backend is created lazily on first use.
func NewRcloneDriverFromCreds(provider, accountID string, creds map[string]string, opts ...AdapterOption) (*RcloneAdapter, error) {
	if strings.TrimSpace(provider) == "" {
		return nil, fmt.Errorf("provider required")
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, fmt.Errorf("account id required")
	}
	p := NormalizeProvider(provider)
	if !IsSupportedProvider(string(p)) {
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
	a := &RcloneAdapter{
		accountID: accountID,
		provider:  p,
		creds:     make(map[string]string, len(creds)),
	}
	for k, v := range creds {
		a.creds[k] = v
	}
	for _, o := range opts {
		o(a)
	}
	return a, nil
}

// NewRcloneDriver parses credentials JSON and returns an adapter.
func NewRcloneDriver(provider, accountID string, credsJSON string, opts ...AdapterOption) (*RcloneAdapter, error) {
	creds := map[string]string{}
	if strings.TrimSpace(credsJSON) != "" {
		if err := json.Unmarshal([]byte(credsJSON), &creds); err != nil {
			return nil, fmt.Errorf("invalid credentials JSON: %w", err)
		}
	}
	return NewRcloneDriverFromCreds(provider, accountID, creds, opts...)
}

// NewRcloneAdapter is a convenience constructor for OAuth-style credentials.
func NewRcloneAdapter(provider, accountID, clientID, clientSecret, accessToken, refreshToken, userEmail, userName string) *RcloneAdapter {
	return NewRcloneAdapterWithExtra(provider, accountID, clientID, clientSecret, accessToken, refreshToken, userEmail, userName, nil)
}

// NewRcloneAdapterWithExtra is NewRcloneAdapter plus provider-specific fields.
func NewRcloneAdapterWithExtra(provider, accountID, clientID, clientSecret, accessToken, refreshToken, userEmail, userName string, extra map[string]string) *RcloneAdapter {
	creds := make(map[string]string, len(extra)+4)
	for k, v := range extra {
		creds[k] = v
	}
	setIf := func(k, v string) {
		if v != "" {
			creds[k] = v
		}
	}
	setIf("client_id", clientID)
	setIf("client_secret", clientSecret)
	setIf("access_token", accessToken)
	setIf("refresh_token", refreshToken)
	a := &RcloneAdapter{
		accountID: accountID,
		provider:  NormalizeProvider(provider),
		creds:     creds,
		userEmail: userEmail,
		userName:  userName,
	}
	return a
}

func (r *RcloneAdapter) ID() string       { return r.accountID }
func (r *RcloneAdapter) Provider() string { return string(r.provider) }

func (r *RcloneAdapter) UserEmail() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.userEmail
}

func (r *RcloneAdapter) UserName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.userName
}

// SetPersister registers the credential persister after construction.
func (r *RcloneAdapter) SetPersister(p CredentialPersister) {
	r.mu.Lock()
	r.persister = p
	r.mu.Unlock()
}

// Credentials returns a snapshot of the current credential map, including any
// state rotated by the backend (tokens, sessions).
func (r *RcloneAdapter) Credentials() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.creds))
	for k, v := range r.creds {
		out[k] = v
	}
	return out
}

// updateCreds mutates credentials under lock and persists the result.
func (r *RcloneAdapter) updateCreds(mutate func(c map[string]string)) {
	r.mu.Lock()
	mutate(r.creds)
	snapshot := make(map[string]string, len(r.creds))
	for k, v := range r.creds {
		snapshot[k] = v
	}
	persister := r.persister
	r.mu.Unlock()
	if persister != nil && !r.memory {
		persister(r.accountID, snapshot)
	}
}

// ── rclone Fs lifecycle ──

func (r *RcloneAdapter) getFs(ctx context.Context) (fs.Fs, error) {
	r.initMu.Lock()
	if r.f != nil {
		f := r.f
		r.initMu.Unlock()
		return f, nil
	}
	if r.initDone == nil {
		if r.lastInitErr != nil && time.Since(r.lastInitAt) < initRetryBackoff {
			err := r.lastInitErr
			r.initMu.Unlock()
			return nil, err
		}
		done := make(chan struct{})
		r.initDone = done
		// Backends keep the context for token refresh, so it must outlive the request.
		initCtx := context.WithoutCancel(ctx)
		go func() {
			f, err := r.newFs(initCtx)
			r.initMu.Lock()
			r.f = f
			r.lastInitErr = err
			r.lastInitAt = time.Now()
			r.initDone = nil
			r.initMu.Unlock()
			close(done)
		}()
	}
	done := r.initDone
	r.initMu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	r.initMu.Lock()
	defer r.initMu.Unlock()
	if r.f != nil {
		return r.f, nil
	}
	if r.lastInitErr != nil {
		return nil, r.lastInitErr
	}
	return nil, ErrDriverNotAvailable
}

func (r *RcloneAdapter) newFs(ctx context.Context) (fs.Fs, error) {
	if r.memory {
		return r.newMemoryFs(ctx)
	}
	if err := r.prepareBackend(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	spec, err := buildBackendConfig(r.provider, r.creds, time.Now())
	if err == nil && r.provider == ProviderSFTP && r.knownHostsFile != "" {
		spec.cfg["known_hosts_file"] = r.knownHostsFile
	}
	r.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	regInfo, err := fs.Find(spec.backend)
	if err != nil {
		return nil, fmt.Errorf("rclone backend %q not available: %w", spec.backend, err)
	}
	mapper := &credMapper{adapter: r, cfg: spec.cfg, defaults: regInfo.Options}
	if r.provider == ProviderS3 {
		// Use LastModified from listings instead of one HEAD request per object.
		var ci *fs.ConfigInfo
		ctx, ci = fs.AddConfig(ctx)
		ci.UseServerModTime = true
	}
	if r.provider == ProviderPikPak && spec.cfg["token"] == "" && regInfo.Config != nil {
		// PikPak signs in with user/pass during `rclone config` (not in NewFs);
		// run that step once to mint the token. mapper.Set persists it.
		if _, err := regInfo.Config(ctx, fsName(r.accountID), mapper, fs.ConfigIn{State: "authorize"}); err != nil {
			return nil, fmt.Errorf("%s: %w", providerLabel(r.provider), err)
		}
	}
	f, err := regInfo.NewFs(ctx, fsName(r.accountID), spec.root, mapper)
	if errors.Is(err, fs.ErrorIsFile) {
		return nil, fmt.Errorf("%s: configured root %q is a file, not a folder", r.provider, spec.root)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", providerLabel(r.provider), err)
	}
	return f, nil
}

func (r *RcloneAdapter) newMemoryFs(ctx context.Context) (fs.Fs, error) {
	regInfo, err := fs.Find("memory")
	if err != nil {
		return nil, err
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	root := "cg-" + sanitizeName(r.accountID) + "-" + hex.EncodeToString(b[:])
	mapper := &credMapper{adapter: r, cfg: map[string]string{}, defaults: regInfo.Options}
	f, err := regInfo.NewFs(ctx, fsName(r.accountID), root, mapper)
	if err != nil {
		return nil, err
	}
	if err := f.Mkdir(ctx, ""); err != nil {
		return nil, err
	}
	return f, nil
}

// prepareBackend performs provider-specific bootstrap steps that rclone
// normally does interactively in `rclone config`.
func (r *RcloneAdapter) prepareBackend(ctx context.Context) error {
	switch r.provider {
	case ProviderOneDrive:
		return r.ensureOneDriveDrive(ctx)
	case ProviderSFTP:
		return r.ensureSFTPHostKey(ctx)
	}
	return nil
}

func fsName(accountID string) string { return "cg_" + sanitizeName(accountID) }

func sanitizeName(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			b.WriteRune(c)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "account"
	}
	return b.String()
}

// credMapper is the configmap.Mapper handed to rclone. Reads resolve against
// the provider config then the backend defaults; writes (token refresh,
// session state) are persisted back into the account credentials.
type credMapper struct {
	adapter  *RcloneAdapter
	cfg      map[string]string
	defaults fs.Options
}

func (m *credMapper) Get(key string) (string, bool) {
	m.adapter.mu.RLock()
	v, ok := m.cfg[key]
	m.adapter.mu.RUnlock()
	if ok {
		return v, true
	}
	if opt := m.defaults.Get(key); opt != nil {
		return opt.String(), true
	}
	return "", false
}

func (m *credMapper) Set(key, value string) {
	m.adapter.mu.Lock()
	old, had := m.cfg[key]
	m.cfg[key] = value
	m.adapter.mu.Unlock()
	if had && old == value {
		return
	}
	m.adapter.updateCreds(func(c map[string]string) { storeBackendState(c, key, value) })
}

// storeBackendState records a value written by the backend into creds.
func storeBackendState(c map[string]string, key, value string) {
	if key == "token" {
		c["token"] = value
		var tok oauth2.Token
		if err := json.Unmarshal([]byte(value), &tok); err == nil {
			if tok.AccessToken != "" {
				c["access_token"] = tok.AccessToken
			}
			if tok.RefreshToken != "" {
				c["refresh_token"] = tok.RefreshToken
			}
			if !tok.Expiry.IsZero() {
				c["token_expiry"] = tok.Expiry.UTC().Format(time.RFC3339)
			}
		}
		return
	}
	c[stateKeyPrefix+key] = value
}

// ── path helpers ──

func relPath(p string) string {
	clean := path.Clean("/" + strings.ReplaceAll(p, "\\", "/"))
	return strings.Trim(clean, "/")
}

func isNotFound(err error) bool {
	return errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorDirNotFound) || errors.Is(err, ErrFileNotFound)
}

func isNotAnObject(err error) bool {
	return errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorIsDir) || errors.Is(err, fs.ErrorNotAFile)
}

// dirExists reports whether rel is an existing directory by looking it up in
// its parent listing (works for both hierarchical and bucket-based backends).
func dirExists(ctx context.Context, f fs.Fs, rel string) (bool, error) {
	if rel == "" {
		return true, nil
	}
	parent := path.Dir(rel)
	if parent == "." {
		parent = ""
	}
	entries, err := f.List(ctx, parent)
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			return false, nil
		}
		return false, err
	}
	for _, e := range entries {
		if d, ok := e.(fs.Directory); ok && d.Remote() == rel {
			return true, nil
		}
	}
	return false, nil
}

func notFound(p string) error { return fmt.Errorf("%w: %s", ErrFileNotFound, p) }

func (r *RcloneAdapter) fileInfo(ctx context.Context, remote string, size int64, isDir bool, mod time.Time) FileInfo {
	if size < 0 {
		size = 0
	}
	return FileInfo{
		Path:      "/" + remote,
		Name:      path.Base("/" + remote),
		Size:      size,
		IsDir:     isDir,
		ModTime:   mod,
		AccountID: r.accountID,
		Provider:  string(r.provider),
	}
}

// ── Driver implementation ──

// About returns quota information. Backends without a quota API are probed
// with a root listing so credential errors are never masked.
func (r *RcloneAdapter) About(ctx context.Context) (QuotaInfo, error) {
	r.quotaMu.Lock()
	if !r.quotaAt.IsZero() && time.Since(r.quotaAt) < quotaCacheTTL {
		q, err := r.quota, r.quotaErr
		r.quotaMu.Unlock()
		return q, err
	}
	r.quotaMu.Unlock()

	q, err := r.fetchQuota(ctx)
	if ctx.Err() != nil {
		// Do not cache results of cancelled requests.
		return q, err
	}
	r.quotaMu.Lock()
	r.quota, r.quotaErr, r.quotaAt = q, err, time.Now()
	r.quotaMu.Unlock()
	return q, err
}

func (r *RcloneAdapter) fetchQuota(ctx context.Context) (QuotaInfo, error) {
	f, err := r.getFs(ctx)
	if err != nil {
		return QuotaInfo{}, err
	}
	var aboutErr error
	if doAbout := f.Features().About; doAbout != nil {
		u, err := doAbout(ctx)
		if err == nil && u != nil {
			if q, ok := quotaFromUsage(u); ok {
				return q, nil
			}
		} else if err != nil {
			aboutErr = err
		}
	}
	// No (usable) quota API: verify connectivity with a root listing.
	if _, err := f.List(ctx, ""); err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
		if aboutErr != nil {
			return QuotaInfo{}, fmt.Errorf("%s: %w", providerLabel(r.provider), aboutErr)
		}
		return QuotaInfo{}, fmt.Errorf("%s: %w", providerLabel(r.provider), err)
	}
	return syntheticQuota(0), nil
}

func quotaFromUsage(u *fs.Usage) (QuotaInfo, bool) {
	val := func(p *int64) (int64, bool) {
		if p == nil || *p < 0 {
			return 0, false
		}
		return *p, true
	}
	total, hasTotal := val(u.Total)
	used, hasUsed := val(u.Used)
	free, hasFree := val(u.Free)
	switch {
	case hasTotal && total > 0:
		if !hasUsed {
			if hasFree {
				used = total - free
			}
		}
		if used > total {
			used = total
		}
		if used < 0 {
			used = 0
		}
		return QuotaInfo{Total: total, Used: used, Free: total - used}, true
	case hasUsed && hasFree:
		return QuotaInfo{Total: used + free, Used: used, Free: free}, true
	case hasUsed:
		return syntheticQuota(used), true
	}
	return QuotaInfo{}, false
}

// TestConnection performs a fresh (uncached) handshake with the provider.
func (r *RcloneAdapter) TestConnection(ctx context.Context) error {
	r.quotaMu.Lock()
	r.quotaAt = time.Time{}
	r.quotaMu.Unlock()
	_, err := r.About(ctx)
	return err
}

func (r *RcloneAdapter) List(ctx context.Context, dirPath string) ([]FileInfo, error) {
	f, err := r.getFs(ctx)
	if err != nil {
		return nil, err
	}
	rel := relPath(dirPath)
	entries, err := f.List(ctx, rel)
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			if rel == "" {
				return []FileInfo{}, nil
			}
			return nil, notFound(dirPath)
		}
		return nil, err
	}
	if len(entries) == 0 && rel != "" {
		// Bucket-based backends (S3, B2) report missing prefixes as empty.
		exists, err := dirExists(ctx, f, rel)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, notFound(dirPath)
		}
	}
	out := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		switch x := e.(type) {
		case fs.Directory:
			out = append(out, r.fileInfo(ctx, x.Remote(), 0, true, x.ModTime(ctx)))
		case fs.Object:
			out = append(out, r.fileInfo(ctx, x.Remote(), x.Size(), false, x.ModTime(ctx)))
		}
	}
	return out, nil
}

func (r *RcloneAdapter) Get(ctx context.Context, filePath string) (io.ReadCloser, FileInfo, error) {
	rel := relPath(filePath)
	if rel == "" {
		return nil, FileInfo{}, fmt.Errorf("cannot download the root folder")
	}
	f, err := r.getFs(ctx)
	if err != nil {
		return nil, FileInfo{}, err
	}
	obj, err := f.NewObject(ctx, rel)
	if err != nil {
		if isNotAnObject(err) {
			return nil, FileInfo{}, notFound(filePath)
		}
		return nil, FileInfo{}, err
	}
	rc, err := obj.Open(ctx)
	if err != nil {
		return nil, FileInfo{}, err
	}
	return rc, r.fileInfo(ctx, obj.Remote(), obj.Size(), false, obj.ModTime(ctx)), nil
}

// Stat returns object metadata without opening a download stream.
func (r *RcloneAdapter) Stat(ctx context.Context, filePath string) (FileInfo, error) {
	rel := relPath(filePath)
	if rel == "" {
		return FileInfo{}, fmt.Errorf("cannot stat the root folder")
	}
	f, err := r.getFs(ctx)
	if err != nil {
		return FileInfo{}, err
	}
	obj, err := f.NewObject(ctx, rel)
	if err != nil {
		if isNotAnObject(err) {
			return FileInfo{}, notFound(filePath)
		}
		return FileInfo{}, err
	}
	return r.fileInfo(ctx, obj.Remote(), obj.Size(), false, obj.ModTime(ctx)), nil
}

func (r *RcloneAdapter) Put(ctx context.Context, filePath string, in io.Reader, size int64) error {
	rel := relPath(filePath)
	if rel == "" {
		return fmt.Errorf("invalid upload path %q", filePath)
	}
	f, err := r.getFs(ctx)
	if err != nil {
		return err
	}
	existing, err := f.NewObject(ctx, rel)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrorObjectNotFound) || errors.Is(err, fs.ErrorDirNotFound):
		existing = nil
	case errors.Is(err, fs.ErrorIsDir) || errors.Is(err, fs.ErrorNotAFile):
		return fmt.Errorf("cannot upload %s: a folder with that name exists", filePath)
	default:
		return err
	}
	now := time.Now()

	if size < 0 {
		if putStream := f.Features().PutStream; putStream != nil && existing == nil {
			_, err := putStream(ctx, in, object.NewStaticObjectInfo(rel, now, -1, true, nil, f))
			r.invalidateQuota()
			return err
		}
		// Unknown size and no streaming support: spool to a temp file to learn the size.
		tmp, err := os.CreateTemp("", "cloudgate-upload-*")
		if err != nil {
			return err
		}
		defer func() {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}()
		n, err := io.Copy(tmp, in)
		if err != nil {
			return err
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return err
		}
		in, size = tmp, n
	}

	info := object.NewStaticObjectInfo(rel, now, size, true, nil, f)
	if existing != nil {
		err = existing.Update(ctx, in, info)
	} else {
		_, err = f.Put(ctx, in, info)
	}
	r.invalidateQuota()
	return err
}

func (r *RcloneAdapter) Delete(ctx context.Context, filePath string) error {
	rel := relPath(filePath)
	if rel == "" {
		return fmt.Errorf("refusing to delete the account root")
	}
	f, err := r.getFs(ctx)
	if err != nil {
		return err
	}
	defer r.invalidateQuota()
	obj, err := f.NewObject(ctx, rel)
	if err == nil {
		return obj.Remove(ctx)
	}
	if !isNotAnObject(err) {
		return err
	}
	exists, err := dirExists(ctx, f, rel)
	if err != nil {
		return err
	}
	if !exists {
		return notFound(filePath)
	}
	return operations.Purge(ctx, f, rel)
}

func (r *RcloneAdapter) Move(ctx context.Context, srcPath, dstPath string) error {
	srcRel, dstRel := relPath(srcPath), relPath(dstPath)
	if srcRel == "" || dstRel == "" {
		return fmt.Errorf("cannot move the account root")
	}
	if srcRel == dstRel {
		return nil
	}
	if strings.HasPrefix(dstRel+"/", srcRel+"/") {
		return fmt.Errorf("cannot move a folder into itself")
	}
	f, err := r.getFs(ctx)
	if err != nil {
		return err
	}
	obj, err := f.NewObject(ctx, srcRel)
	if err == nil {
		_, err = operations.Move(ctx, f, nil, dstRel, obj)
		return err
	}
	if !isNotAnObject(err) {
		return err
	}
	exists, err := dirExists(ctx, f, srcRel)
	if err != nil {
		return err
	}
	if !exists {
		return notFound(srcPath)
	}
	if dirMove := f.Features().DirMove; dirMove != nil {
		err := dirMove(ctx, f, srcRel, dstRel)
		if err == nil {
			return nil
		}
		if !errors.Is(err, fs.ErrorCantDirMove) {
			return err
		}
	}
	if err := r.moveDirRecursive(ctx, f, srcRel, dstRel); err != nil {
		return err
	}
	return operations.Rmdirs(ctx, f, srcRel, false)
}

func (r *RcloneAdapter) moveDirRecursive(ctx context.Context, f fs.Fs, srcRel, dstRel string) error {
	if err := f.Mkdir(ctx, dstRel); err != nil {
		return err
	}
	entries, err := f.List(ctx, srcRel)
	if err != nil {
		return err
	}
	for _, e := range entries {
		target := path.Join(dstRel, path.Base(e.Remote()))
		switch x := e.(type) {
		case fs.Directory:
			if err := r.moveDirRecursive(ctx, f, x.Remote(), target); err != nil {
				return err
			}
		case fs.Object:
			if _, err := operations.Move(ctx, f, nil, target, x); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RcloneAdapter) Mkdir(ctx context.Context, dirPath string) error {
	f, err := r.getFs(ctx)
	if err != nil {
		return err
	}
	return f.Mkdir(ctx, relPath(dirPath))
}

// GetShareLink returns the gateway download URL (requires GatewayAuth).
func (r *RcloneAdapter) GetShareLink(ctx context.Context, filePath string) (string, error) {
	return gatewayDownloadLink(r.accountID, filePath), nil
}

// PublicLink asks the provider for a native public link (anyone with the link
// can access it). Returns an error when the provider does not support it.
func (r *RcloneAdapter) PublicLink(ctx context.Context, filePath string) (string, error) {
	f, err := r.getFs(ctx)
	if err != nil {
		return "", err
	}
	if f.Features().PublicLink == nil {
		return "", fmt.Errorf("%s does not support public links", providerLabel(r.provider))
	}
	return operations.PublicLink(ctx, f, relPath(filePath), fs.DurationOff, false)
}

func (r *RcloneAdapter) invalidateQuota() {
	r.quotaMu.Lock()
	r.quotaAt = time.Time{}
	r.quotaMu.Unlock()
}

func gatewayDownloadLink(accountID, filePath string) string {
	return fmt.Sprintf("/api/files/download?path=%s&account_id=%s", url.QueryEscape(filePath), url.QueryEscape(accountID))
}

// ── Unavailable driver ──

// UnavailableDriver represents a registered account whose backend cannot be
// constructed (e.g. missing or corrupt credentials). Every operation fails
// loudly instead of silently falling back to in-memory storage.
type UnavailableDriver struct {
	id       string
	provider string
	reason   error
}

// NewUnavailableDriver creates a driver that always reports reason.
func NewUnavailableDriver(id, provider string, reason error) *UnavailableDriver {
	if reason == nil {
		reason = ErrDriverNotAvailable
	}
	return &UnavailableDriver{id: id, provider: provider, reason: fmt.Errorf("%w: %v", ErrDriverNotAvailable, reason)}
}

func (u *UnavailableDriver) ID() string       { return u.id }
func (u *UnavailableDriver) Provider() string { return u.provider }
func (u *UnavailableDriver) About(context.Context) (QuotaInfo, error) {
	return QuotaInfo{}, u.reason
}
func (u *UnavailableDriver) List(context.Context, string) ([]FileInfo, error) { return nil, u.reason }
func (u *UnavailableDriver) Get(context.Context, string) (io.ReadCloser, FileInfo, error) {
	return nil, FileInfo{}, u.reason
}
func (u *UnavailableDriver) Put(context.Context, string, io.Reader, int64) error { return u.reason }
func (u *UnavailableDriver) Delete(context.Context, string) error                { return u.reason }
func (u *UnavailableDriver) Move(context.Context, string, string) error          { return u.reason }
func (u *UnavailableDriver) Mkdir(context.Context, string) error                 { return u.reason }
func (u *UnavailableDriver) TestConnection(context.Context) error                { return u.reason }
func (u *UnavailableDriver) GetShareLink(ctx context.Context, p string) (string, error) {
	return gatewayDownloadLink(u.id, p), nil
}
