package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rclone/rclone/fs/config/obscure"
	"golang.org/x/crypto/ssh"
	"golang.org/x/oauth2"
)

func TestRcloneDriverFactory(t *testing.T) {
	creds := `{"client_id":"cid","client_secret":"cs","access_token":"at","refresh_token":"rt"}`
	for _, prov := range []string{"onedrive", "dropbox", "google"} {
		drv, err := NewRcloneDriver(prov, "acc_"+prov, creds)
		if err != nil {
			t.Fatalf("factory failed for %s: %v", prov, err)
		}
		want := string(NormalizeProvider(prov))
		if drv.Provider() != want || drv.ID() != "acc_"+prov {
			t.Errorf("unexpected provider/id %s/%s", drv.Provider(), drv.ID())
		}
	}
	if _, err := NewRcloneDriver("", "id", creds); err == nil {
		t.Error("expected error for empty provider")
	}
	if _, err := NewRcloneDriver("nosuch", "id", creds); err == nil {
		t.Error("expected error for unsupported provider")
	}
	if _, err := NewRcloneDriver("s3", "id", "{not json"); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestMemoryBackendOperations exercises the generic Driver implementation for
// every provider through rclone's memory backend.
func TestMemoryBackendOperations(t *testing.T) {
	for _, p := range SupportedProviders {
		p := p
		t.Run(string(p), func(t *testing.T) {
			ctx := context.Background()
			drv, err := NewRcloneDriverFromCreds(string(p), "acc_mem_"+string(p), nil, WithMemoryBackend())
			if err != nil {
				t.Fatal(err)
			}
			q, err := drv.About(ctx)
			if err != nil || q.Total <= 0 {
				t.Fatalf("about: %+v %v", q, err)
			}
			if err := drv.Put(ctx, "/docs/a.txt", bytes.NewReader([]byte("hello")), 5); err != nil {
				t.Fatalf("put: %v", err)
			}
			// Unknown-size upload.
			if err := drv.Put(ctx, "/docs/sub/b.txt", strings.NewReader("streamed"), -1); err != nil {
				t.Fatalf("put stream: %v", err)
			}
			// Overwrite existing object.
			if err := drv.Put(ctx, "/docs/a.txt", bytes.NewReader([]byte("hello2")), 6); err != nil {
				t.Fatalf("overwrite: %v", err)
			}
			files, err := drv.List(ctx, "/docs")
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			names := map[string]FileInfo{}
			for _, f := range files {
				names[f.Name] = f
			}
			if fa, ok := names["a.txt"]; !ok || fa.IsDir || fa.Size != 6 || fa.Path != "/docs/a.txt" {
				t.Fatalf("unexpected a.txt entry: %+v (all: %+v)", fa, files)
			}
			if fs, ok := names["sub"]; !ok || !fs.IsDir {
				t.Fatalf("expected sub folder, got %+v", files)
			}
			rc, info, err := drv.Get(ctx, "/docs/a.txt")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if string(b) != "hello2" || info.Name != "a.txt" || info.Size != 6 {
				t.Fatalf("unexpected get %q %+v", b, info)
			}
			if _, _, err := drv.Get(ctx, "/docs/missing.txt"); !errors.Is(err, ErrFileNotFound) {
				t.Fatalf("expected ErrFileNotFound, got %v", err)
			}
			// File move across folders (not just rename).
			if err := drv.Move(ctx, "/docs/a.txt", "/archive/a-moved.txt"); err != nil {
				t.Fatalf("move file: %v", err)
			}
			if _, _, err := drv.Get(ctx, "/archive/a-moved.txt"); err != nil {
				t.Fatalf("moved file missing: %v", err)
			}
			if _, _, err := drv.Get(ctx, "/docs/a.txt"); !errors.Is(err, ErrFileNotFound) {
				t.Fatalf("source should be gone, got %v", err)
			}
			// Folder move.
			if err := drv.Move(ctx, "/docs/sub", "/archive/sub2"); err != nil {
				t.Fatalf("move dir: %v", err)
			}
			if _, _, err := drv.Get(ctx, "/archive/sub2/b.txt"); err != nil {
				t.Fatalf("moved dir content missing: %v", err)
			}
			if err := drv.Move(ctx, "/nope", "/nope2"); !errors.Is(err, ErrFileNotFound) {
				t.Fatalf("expected not found on move, got %v", err)
			}
			if err := drv.Mkdir(ctx, "/new/nested"); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			// Recursive folder delete.
			if err := drv.Delete(ctx, "/archive"); err != nil {
				t.Fatalf("delete dir: %v", err)
			}
			if _, err := drv.List(ctx, "/archive"); !errors.Is(err, ErrFileNotFound) {
				t.Fatalf("expected archive gone, got %v", err)
			}
			if err := drv.Delete(ctx, "/archive"); !errors.Is(err, ErrFileNotFound) {
				t.Fatalf("expected not found on second delete, got %v", err)
			}
			if err := drv.Delete(ctx, "/"); err == nil {
				t.Fatal("deleting root must be refused")
			}
			if err := drv.TestConnection(ctx); err != nil {
				t.Fatalf("test connection: %v", err)
			}
			link, _ := drv.GetShareLink(ctx, "/x y.txt")
			if !strings.Contains(link, "account_id=acc_mem_") || !strings.Contains(link, "x+y.txt") {
				t.Fatalf("unexpected share link %s", link)
			}
		})
	}
}

// TestNoSilentFallbackOnAuthFailure verifies that real backend errors surface
// instead of being swallowed into an in-memory store.
func TestNoSilentFallbackOnAuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer ts.Close()
	drv, err := NewRcloneDriverFromCreds("webdav", "acc_dav_bad", map[string]string{"url": ts.URL, "username": "u", "password": "mockingbird"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := drv.Put(ctx, "/a.txt", strings.NewReader("x"), 1); err == nil {
		t.Fatal("expected Put to fail against 401 server")
	}
	if _, err := drv.List(ctx, "/"); err == nil {
		t.Fatal("expected List to fail against 401 server")
	}
	if err := drv.TestConnection(ctx); err == nil {
		t.Fatal("expected TestConnection to fail against 401 server")
	}
}

func TestWebDAVLiveAgainstFakeServer(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if u, p, ok := r.BasicAuth(); !ok || u != "alice" || p != "s3cret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(207)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">
<d:response><d:href>/dav/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
<d:response><d:href>/dav/My%20File.txt</d:href><d:propstat><d:prop><d:getcontentlength>3</d:getcontentlength><d:getlastmodified>Mon, 28 Sep 2026 10:00:00 GMT</d:getlastmodified><d:resourcetype/></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`)
		default:
			w.WriteHeader(http.StatusNotImplemented)
		}
	}))
	defer ts.Close()
	drv, _ := NewRcloneDriverFromCreds("webdav", "acc_dav", map[string]string{"url": ts.URL + "/dav/", "username": "alice", "password": "s3cret"})
	files, err := drv.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 1 || files[0].Name != "My File.txt" || files[0].Size != 3 {
		t.Fatalf("expected decoded href entry, got %+v", files)
	}
}

func TestS3SignsRequests(t *testing.T) {
	var sawAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if !strings.HasPrefix(sawAuth, "AWS4-HMAC-SHA256 Credential=AKTEST/") {
			http.Error(w, "<Error><Code>AccessDenied</Code></Error>", 403)
			return
		}
		if r.URL.Path != "/bk" && r.URL.Path != "/bk/" {
			http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", 404)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>bk</Name><Prefix></Prefix><KeyCount>1</KeyCount><MaxKeys>1000</MaxKeys><Delimiter>/</Delimiter><IsTruncated>false</IsTruncated><Contents><Key>x.txt</Key><LastModified>2026-09-28T10:00:00.000Z</LastModified><ETag>"abc"</ETag><Size>3</Size><StorageClass>STANDARD</StorageClass></Contents><CommonPrefixes><Prefix>photos/</Prefix></CommonPrefixes></ListBucketResult>`)
	}))
	defer ts.Close()
	drv, _ := NewRcloneDriverFromCreds("s3", "acc_s3", map[string]string{"endpoint": ts.URL, "s3_provider": "Minio", "bucket": "bk", "access_key_id": "AKTEST", "secret_access_key": "SECRET", "region": "us-east-1"})
	files, err := drv.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("list: %v (auth header %q)", err, sawAuth)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Name] = f.IsDir
	}
	if isDir, ok := got["x.txt"]; !ok || isDir {
		t.Fatalf("missing object: %+v", files)
	}
	if isDir, ok := got["photos"]; !ok || !isDir {
		t.Fatalf("common prefix not mapped to folder: %+v", files)
	}
}

func reveal(t *testing.T, s string) string {
	t.Helper()
	v, err := obscure.Reveal(s)
	if err != nil {
		t.Fatalf("value %q is not obscured: %v", s, err)
	}
	return v
}

func TestBuildBackendConfig(t *testing.T) {
	now := time.Unix(59, 0)
	t.Run("b2 key is not obscured", func(t *testing.T) {
		s, err := buildBackendConfig(ProviderB2, map[string]string{"key_id": "K1", "application_key": "APPKEY", "bucket": "bk"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if s.backend != "b2" || s.cfg["account"] != "K1" || s.cfg["key"] != "APPKEY" || s.root != "bk" || s.cfg["hard_delete"] != "false" {
			t.Fatalf("unexpected %+v", s)
		}
	})
	t.Run("s3 signs with keys, bucket root, provider inference", func(t *testing.T) {
		s, err := buildBackendConfig(ProviderS3, map[string]string{"endpoint": "https://abc.r2.cloudflarestorage.com", "bucket": "media", "access_key_id": "AK", "secret_access_key": "SK"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if s.cfg["provider"] != "Cloudflare" || s.cfg["region"] != "auto" || s.cfg["access_key_id"] != "AK" || s.cfg["secret_access_key"] != "SK" || s.root != "media" || s.cfg["no_check_bucket"] != "true" {
			t.Fatalf("unexpected %+v", s)
		}
		s, _ = buildBackendConfig(ProviderS3, map[string]string{"bucket": "b", "access_key": "AK", "secret_key": "SK"}, now)
		if s.cfg["provider"] != "AWS" || s.cfg["region"] != "us-east-1" || s.cfg["access_key_id"] != "AK" {
			t.Fatalf("legacy aliases not mapped: %+v", s)
		}
		if _, err := buildBackendConfig(ProviderS3, map[string]string{"access_key_id": "a"}, now); err == nil {
			t.Fatal("expected bucket error")
		}
	})
	t.Run("proton otp secret is its own option", func(t *testing.T) {
		s, err := buildBackendConfig(ProviderProtonDrive, map[string]string{"username": "u@proton.me", "password": "pw", "otp_secret_key": "jbsw y3dp", "mailbox_password": "mb", "rclone.client_uid": "UID"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, has := s.cfg["2fa"]; has {
			t.Fatal("otp secret must not be sent as 2fa code")
		}
		if reveal(t, s.cfg["otp_secret_key"]) != "JBSWY3DP" || reveal(t, s.cfg["password"]) != "pw" || reveal(t, s.cfg["mailbox_password"]) != "mb" {
			t.Fatalf("unexpected %+v", s.cfg)
		}
		if s.cfg["client_uid"] != "UID" {
			t.Fatal("persisted proton session state not restored")
		}
	})
	t.Run("pikpak root_folder_id is an option", func(t *testing.T) {
		s, _ := buildBackendConfig(ProviderPikPak, map[string]string{"user": "u", "pass": "p", "root_folder_id": "VN123", "rclone.device_id": "dev"}, now)
		if s.root != "" || s.cfg["root_folder_id"] != "VN123" || reveal(t, s.cfg["pass"]) != "p" || s.cfg["device_id"] != "dev" {
			t.Fatalf("unexpected %+v", s)
		}
	})
	t.Run("smb root joins share and path", func(t *testing.T) {
		s, _ := buildBackendConfig(ProviderSMB, map[string]string{"host": "h", "user": "u", "pass": "p", "share": "backups", "path": "/team/x"}, now)
		if s.root != "backups/team/x" || s.cfg["domain"] != "WORKGROUP" || s.cfg["port"] != "445" {
			t.Fatalf("unexpected %+v", s)
		}
		if _, err := buildBackendConfig(ProviderSMB, map[string]string{"host": "h", "user": "u"}, now); err == nil {
			t.Fatal("share must be required")
		}
	})
	t.Run("sftp secrets obscured, pem kept", func(t *testing.T) {
		s, _ := buildBackendConfig(ProviderSFTP, map[string]string{"host": "h", "username": "u", "private_key": "-----BEGIN-----\r\nabc\r\n-----END-----", "passphrase": "pp"}, now)
		if s.cfg["port"] != "22" || !strings.Contains(s.cfg["key_pem"], "\nabc\n") || reveal(t, s.cfg["key_file_pass"]) != "pp" {
			t.Fatalf("unexpected %+v", s.cfg)
		}
	})
	t.Run("mega totp", func(t *testing.T) {
		s, err := buildBackendConfig(ProviderMega, map[string]string{"email": "m@x", "password": "p", "otp_secret_key": "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if s.cfg["2fa"] != "287082" || s.cfg["user"] != "m@x" || reveal(t, s.cfg["pass"]) != "p" {
			t.Fatalf("unexpected %+v", s.cfg)
		}
	})
	t.Run("koofr uses native backend", func(t *testing.T) {
		s, _ := buildBackendConfig(ProviderKoofr, map[string]string{"url": "https://app.koofr.net/dav/Koofr", "username": "k@x", "password": "app"}, now)
		if s.backend != "koofr" || s.cfg["provider"] != "koofr" || s.cfg["user"] != "k@x" || reveal(t, s.cfg["password"]) != "app" {
			t.Fatalf("unexpected %+v", s)
		}
	})
	t.Run("webdav vendor detection", func(t *testing.T) {
		s, _ := buildBackendConfig(ProviderWebDAV, map[string]string{"url": "https://c.example.com/remote.php/dav/files/bob/", "username": "bob", "password": "p"}, now)
		if s.cfg["vendor"] != "nextcloud" || reveal(t, s.cfg["pass"]) != "p" {
			t.Fatalf("unexpected %+v", s.cfg)
		}
	})
	t.Run("pcloud host", func(t *testing.T) {
		base := map[string]string{"client_id": "c", "client_secret": "s", "access_token": "a"}
		s, _ := buildBackendConfig(ProviderPCloud, base, now)
		if s.cfg["hostname"] != "api.pcloud.com" {
			t.Fatalf("unexpected %+v", s.cfg)
		}
		base["hostname"] = "eapi.pcloud.com"
		s, _ = buildBackendConfig(ProviderPCloud, base, now)
		if s.cfg["hostname"] != "eapi.pcloud.com" {
			t.Fatalf("unexpected %+v", s.cfg)
		}
		base["hostname"] = "evil.example.com"
		if _, err := buildBackendConfig(ProviderPCloud, base, now); err == nil {
			t.Fatal("unexpected host must be rejected")
		}
	})
	t.Run("legacy oauth token forces refresh", func(t *testing.T) {
		s, err := buildBackendConfig(ProviderGDrive, map[string]string{"client_id": "c", "client_secret": "s", "access_token": "old", "refresh_token": "rt"}, now)
		if err != nil {
			t.Fatal(err)
		}
		var tok oauth2.Token
		_ = json.Unmarshal([]byte(s.cfg["token"]), &tok)
		if s.backend != "drive" || tok.RefreshToken != "rt" || !tok.Expiry.Before(time.Now()) {
			t.Fatalf("unexpected token %+v", tok)
		}
	})
	t.Run("onedrive needs drive id", func(t *testing.T) {
		if _, err := buildBackendConfig(ProviderOneDrive, map[string]string{"client_id": "c", "client_secret": "s", "access_token": "a"}, now); err == nil {
			t.Fatal("expected drive id error")
		}
	})
}

func TestValidateCredentials(t *testing.T) {
	ok := map[string]map[string]string{
		"s3":          {"bucket": "b", "access_key_id": "a", "secret_access_key": "s"},
		"webdav":      {"url": "https://x/dav"},
		"koofr":       {"username": "u", "password": "p"},
		"mega":        {"email": "u", "password": "p"},
		"filen":       {"email": "u", "password": "p", "api_key": "k"},
		"b2":          {"key_id": "a", "application_key": "b"},
		"pikpak":      {"user": "u", "pass": "p"},
		"sftp":        {"host": "h", "user": "u", "pass": "p"},
		"smb":         {"host": "h", "user": "u", "share": "s"},
		"protondrive": {"username": "u", "password": "p"},
		"gdrive":      {"client_id": "c", "client_secret": "s", "token": `{"access_token":"a"}`},
	}
	for p, c := range ok {
		if err := ValidateCredentials(p, c); err != nil {
			t.Errorf("%s: unexpected error %v", p, err)
		}
	}
	bad := map[string]map[string]string{
		"s3":          {"bucket": "b"},
		"webdav":      {"url": "ftp://x"},
		"filen":       {"email": "u", "api_key": "k"},
		"sftp":        {"host": "h", "user": "u"},
		"smb":         {"host": "h", "user": "u"},
		"mega":        {"email": "u"},
		"protondrive": {"username": "u"},
		"onedrive":    {"client_id": "c"},
	}
	for p, c := range bad {
		if err := ValidateCredentials(p, c); err == nil {
			t.Errorf("%s: expected validation error", p)
		}
	}
}

func TestTOTPVector(t *testing.T) {
	// RFC 6238 SHA1 vector (T=59s -> 94287082, last 6 digits).
	code, err := totpCode("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", time.Unix(59, 0))
	if err != nil || code != "287082" {
		t.Fatalf("got %s %v", code, err)
	}
	if _, err := totpCode("not base32 !!", time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCredMapperPersistsRotatedToken(t *testing.T) {
	var mu sync.Mutex
	var saved map[string]string
	a, _ := NewRcloneDriverFromCreds("box", "acc_box", map[string]string{"client_id": "c", "client_secret": "s", "access_token": "a1", "refresh_token": "r1"},
		WithPersister(func(id string, c map[string]string) {
			mu.Lock()
			saved = c
			mu.Unlock()
		}))
	m := &credMapper{adapter: a, cfg: map[string]string{}}
	m.Set("token", `{"access_token":"a2","token_type":"Bearer","refresh_token":"r2","expiry":"2030-01-01T00:00:00Z"}`)
	m.Set("client_uid", "uid-1")
	mu.Lock()
	defer mu.Unlock()
	if saved["access_token"] != "a2" || saved["refresh_token"] != "r2" || saved["token_expiry"] != "2030-01-01T00:00:00Z" || saved["rclone.client_uid"] != "uid-1" {
		t.Fatalf("rotated state not persisted: %+v", saved)
	}
	if got := a.Credentials()["refresh_token"]; got != "r2" {
		t.Fatalf("adapter creds not updated: %s", got)
	}
}

func TestEnsureOneDriveDrive(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("refresh_token") != "rt" || r.Form.Get("client_id") != "cid" {
				http.Error(w, `{"error":"invalid_grant"}`, 400)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"fresh","refresh_token":"rt2","token_type":"Bearer","expires_in":3600}`)
		case "/v1.0/me/drive":
			if r.Header.Get("Authorization") != "Bearer fresh" {
				http.Error(w, "unauthorized", 401)
				return
			}
			_, _ = io.WriteString(w, `{"id":"DRIVE123","driveType":"personal"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	oldTok, oldGraph := oneDriveTokenURL, msGraphBaseURL
	oneDriveTokenURL, msGraphBaseURL = ts.URL+"/token", ts.URL
	defer func() { oneDriveTokenURL, msGraphBaseURL = oldTok, oldGraph }()

	var saved map[string]string
	a, _ := NewRcloneDriverFromCreds("onedrive", "acc_od", map[string]string{"client_id": "cid", "client_secret": "cs", "access_token": "stale", "refresh_token": "rt"},
		WithPersister(func(_ string, c map[string]string) { saved = c }))
	if err := a.ensureOneDriveDrive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if saved["drive_id"] != "DRIVE123" || saved["drive_type"] != "personal" || saved["refresh_token"] != "rt2" {
		t.Fatalf("unexpected persisted creds %+v", saved)
	}
}

func TestSFTPHostKeyPinning(t *testing.T) {
	signer, err := ssh.NewSignerFromKey(testEd25519Key(t))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	old := sshHostKeyFetcher
	sshHostKeyFetcher = func(ctx context.Context, addr string) (ssh.PublicKey, error) {
		calls++
		if addr != "files.example.com:2222" {
			t.Errorf("unexpected addr %s", addr)
		}
		return signer.PublicKey(), nil
	}
	defer func() { sshHostKeyFetcher = old }()

	var saved map[string]string
	a, _ := NewRcloneDriverFromCreds("sftp", "acc_sftp", map[string]string{"host": "files.example.com", "port": "2222", "user": "u", "pass": "p"},
		WithPersister(func(_ string, c map[string]string) { saved = c }))
	if err := a.ensureSFTPHostKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(saved["known_host_key"], "[files.example.com]:2222 ssh-ed25519 ") || saved["host_key_fingerprint"] == "" {
		t.Fatalf("host key not pinned: %+v", saved)
	}
	if a.knownHostsFile == "" {
		t.Fatal("known_hosts file not prepared")
	}
	// Second bootstrap must reuse the pinned key, not re-trust the network.
	if err := a.ensureSFTPHostKey(context.Background()); err != nil || calls != 1 {
		t.Fatalf("expected pinned key reuse, calls=%d err=%v", calls, err)
	}
}

func TestUnavailableDriver(t *testing.T) {
	d := NewUnavailableDriver("acc", "s3", errors.New("missing credentials"))
	if err := d.Put(context.Background(), "/a", strings.NewReader(""), 0); !errors.Is(err, ErrDriverNotAvailable) {
		t.Fatalf("expected ErrDriverNotAvailable, got %v", err)
	}
	if _, err := d.About(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestQuotaFromUsage(t *testing.T) {
	i := func(v int64) *int64 { return &v }
	cases := []struct {
		total, used, free *int64
		want              QuotaInfo
		ok                bool
	}{
		{i(100), i(40), nil, QuotaInfo{100, 40, 60}, true},
		{i(100), nil, i(30), QuotaInfo{100, 70, 30}, true},
		{nil, i(5), i(10), QuotaInfo{15, 5, 10}, true},
		{nil, nil, nil, QuotaInfo{}, false},
	}
	for _, c := range cases {
		u := usageT{c.total, c.used, c.free}.toUsage()
		got, ok := quotaFromUsage(&u)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("got %+v %v want %+v", got, ok, c.want)
		}
	}
	keys := []string{}
	for _, p := range SupportedProviders {
		keys = append(keys, string(p))
	}
	sort.Strings(keys)
	if len(keys) != 16 {
		t.Fatalf("expected 16 providers, got %d", len(keys))
	}
}
