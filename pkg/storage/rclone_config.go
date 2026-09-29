package storage

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/rclone/rclone/fs/config/obscure"
	"golang.org/x/oauth2"
)

// stateKeyPrefix namespaces values written back by rclone backends (Proton
// session, PikPak device id, ...) inside accounts.credentials.
const stateKeyPrefix = "rclone."

// backendSpec is the fully resolved rclone configuration for one account.
type backendSpec struct {
	backend string            // rclone backend name (fs registry)
	root    string            // root path passed to NewFs (bucket, share, folder)
	cfg     map[string]string // rclone option values (passwords already obscured)
}

func providerLabel(p Provider) string {
	switch p {
	case ProviderGDrive:
		return "Google Drive"
	case ProviderOneDrive:
		return "OneDrive"
	case ProviderDropbox:
		return "Dropbox"
	case ProviderBox:
		return "Box"
	case ProviderPCloud:
		return "pCloud"
	case ProviderYandex:
		return "Yandex Disk"
	case ProviderKoofr:
		return "Koofr"
	case ProviderS3:
		return "S3"
	case ProviderWebDAV:
		return "WebDAV"
	case ProviderMega:
		return "MEGA"
	case ProviderFilen:
		return "Filen"
	case ProviderB2:
		return "Backblaze B2"
	case ProviderPikPak:
		return "PikPak"
	case ProviderSFTP:
		return "SFTP"
	case ProviderSMB:
		return "SMB"
	case ProviderProtonDrive:
		return "Proton Drive"
	}
	return string(p)
}

// first returns the first non-empty (trimmed) value among keys.
func first(c map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(c[k]); v != "" {
			return v
		}
	}
	return ""
}

// firstRaw is like first but does not trim (for passwords / PEM keys).
func firstRaw(c map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := c[k]; strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func obscureValue(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	return obscure.Obscure(v)
}

// Credential key aliases accepted from the UI / API (first match wins).
var (
	keysUser     = []string{"username", "user", "email"}
	keysPass     = []string{"password", "pass"}
	keysS3Access = []string{"access_key_id", "access_key"}
	keysS3Secret = []string{"secret_access_key", "secret_key"}
)

// ValidateCredentials checks that the required fields for provider are present.
func ValidateCredentials(provider string, c map[string]string) error {
	p := NormalizeProvider(provider)
	missing := func(what string) error { return fmt.Errorf("%s requires %s", providerLabel(p), what) }
	switch p {
	case ProviderGDrive, ProviderOneDrive, ProviderDropbox, ProviderBox, ProviderPCloud, ProviderYandex:
		if first(c, "client_id") == "" || first(c, "client_secret") == "" {
			return missing("OAuth Client ID and Client Secret")
		}
		if first(c, "token", "access_token", "refresh_token") == "" {
			return missing("an OAuth token (connect through the OAuth sign-in flow)")
		}
	case ProviderS3:
		if first(c, "bucket") == "" {
			return missing("bucket")
		}
		if first(c, keysS3Access...) == "" || firstRaw(c, keysS3Secret...) == "" {
			return missing("access key ID and secret access key")
		}
	case ProviderWebDAV:
		if first(c, "url") == "" {
			return missing("url")
		}
		if u, err := url.Parse(first(c, "url")); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("WebDAV url must be an absolute http(s) URL")
		}
	case ProviderKoofr:
		if first(c, keysUser...) == "" || firstRaw(c, keysPass...) == "" {
			return missing("email and app password")
		}
	case ProviderMega:
		if first(c, keysUser...) == "" || firstRaw(c, keysPass...) == "" {
			return missing("email and password")
		}
		if s := first(c, "otp_secret_key"); s != "" {
			if _, err := totpCode(s, time.Now()); err != nil {
				return fmt.Errorf("MEGA OTP secret key is invalid: %v", err)
			}
		}
	case ProviderFilen:
		if first(c, "email", "username") == "" || firstRaw(c, "password", "pass", "filen_pass") == "" || firstRaw(c, "api_key", "filen_api_key") == "" {
			return missing("email, password and API key")
		}
	case ProviderB2:
		if first(c, "account", "key_id", "username") == "" || firstRaw(c, "key", "application_key", "password") == "" {
			return missing("application key ID and application key")
		}
	case ProviderPikPak:
		if first(c, "user", "username", "email") == "" || firstRaw(c, "pass", "password") == "" {
			return missing("username/email and password")
		}
	case ProviderSFTP:
		if first(c, "host") == "" || first(c, "user", "username") == "" {
			return missing("host and user")
		}
		if firstRaw(c, "pass", "password") == "" && firstRaw(c, "key_pem", "private_key") == "" && first(c, "key_file") == "" {
			return missing("a password or a private key")
		}
		if port := first(c, "port"); port != "" {
			if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
				return fmt.Errorf("SFTP port must be between 1 and 65535")
			}
		}
	case ProviderSMB:
		if first(c, "host") == "" || first(c, "user", "username") == "" || first(c, "share") == "" {
			return missing("host, user and share name")
		}
	case ProviderProtonDrive:
		if first(c, "username", "user", "email") == "" || firstRaw(c, "password", "pass") == "" {
			return missing("username and password")
		}
	default:
		return fmt.Errorf("unsupported provider: %s", provider)
	}
	return nil
}

// tokenFromCreds returns the OAuth token stored in creds (rclone JSON form or
// the legacy access_token/refresh_token pair).
func tokenFromCreds(c map[string]string) *oauth2.Token {
	if t := strings.TrimSpace(c["token"]); t != "" {
		var tok oauth2.Token
		if err := json.Unmarshal([]byte(t), &tok); err == nil && (tok.AccessToken != "" || tok.RefreshToken != "") {
			return &tok
		}
	}
	at, rt := strings.TrimSpace(c["access_token"]), strings.TrimSpace(c["refresh_token"])
	if at == "" && rt == "" {
		return nil
	}
	tok := &oauth2.Token{AccessToken: at, RefreshToken: rt, TokenType: "Bearer"}
	if exp := strings.TrimSpace(c["token_expiry"]); exp != "" {
		if t, err := time.Parse(time.RFC3339, exp); err == nil {
			tok.Expiry = t
		}
	}
	if tok.Expiry.IsZero() && rt != "" {
		// Unknown age: force a refresh on first use.
		tok.Expiry = time.Now().Add(-time.Minute)
	}
	return tok
}

// OAuthTokenJSON encodes an OAuth token in the format rclone backends expect.
func OAuthTokenJSON(accessToken, refreshToken, tokenType string, expiresIn int) string {
	if tokenType == "" || strings.EqualFold(tokenType, "bearer") {
		tokenType = "Bearer"
	}
	tok := oauth2.Token{AccessToken: accessToken, RefreshToken: refreshToken, TokenType: tokenType}
	if expiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	b, _ := json.Marshal(tok)
	return string(b)
}

func tokenJSON(tok *oauth2.Token) string {
	b, _ := json.Marshal(tok)
	return string(b)
}

// inferS3Provider guesses the rclone S3 provider from the endpoint host.
func inferS3Provider(endpoint string) string {
	e := strings.ToLower(endpoint)
	switch {
	case e == "" || strings.Contains(e, "amazonaws.com"):
		return "AWS"
	case strings.Contains(e, "r2.cloudflarestorage.com"):
		return "Cloudflare"
	case strings.Contains(e, "wasabisys.com"):
		return "Wasabi"
	case strings.Contains(e, "digitaloceanspaces.com"):
		return "DigitalOcean"
	case strings.Contains(e, "storage.googleapis.com"):
		return "GCS"
	case strings.Contains(e, "scw.cloud"):
		return "Scaleway"
	case strings.Contains(e, "linodeobjects.com"):
		return "Linode"
	case strings.Contains(e, "idrivee2"):
		return "IDrive"
	case strings.Contains(e, "your-objectstorage.com"):
		return "Hetzner"
	}
	return "Other"
}

var validS3Providers = map[string]bool{
	"AWS": true, "Cloudflare": true, "Wasabi": true, "DigitalOcean": true, "GCS": true, "Scaleway": true,
	"Linode": true, "IDrive": true, "Hetzner": true, "Minio": true, "Ceph": true, "Other": true,
}

// buildBackendConfig maps CloudGate credentials onto rclone backend options.
func buildBackendConfig(p Provider, c map[string]string, now time.Time) (backendSpec, error) {
	cfg := map[string]string{}
	spec := backendSpec{cfg: cfg}
	setObscured := func(key, value string) error {
		if value == "" {
			return nil
		}
		ob, err := obscureValue(value)
		if err != nil {
			return err
		}
		cfg[key] = ob
		return nil
	}
	oauth := func(backend string) error {
		spec.backend = backend
		cfg["client_id"] = first(c, "client_id")
		cfg["client_secret"] = firstRaw(c, "client_secret")
		tok := tokenFromCreds(c)
		if cfg["client_id"] == "" || cfg["client_secret"] == "" {
			return fmt.Errorf("%s: missing OAuth client ID/secret - reconnect the account", providerLabel(p))
		}
		if tok == nil {
			return fmt.Errorf("%s: missing OAuth token - reconnect the account", providerLabel(p))
		}
		cfg["token"] = tokenJSON(tok)
		return nil
	}

	var err error
	switch p {
	case ProviderGDrive:
		err = oauth("drive")
		cfg["scope"] = "drive"
		if v := first(c, "root_folder_id"); v != "" {
			cfg["root_folder_id"] = v
		}
	case ProviderOneDrive:
		err = oauth("onedrive")
		cfg["drive_id"] = first(c, "drive_id")
		cfg["drive_type"] = first(c, "drive_type")
		cfg["region"] = "global"
		if err == nil && (cfg["drive_id"] == "" || cfg["drive_type"] == "") {
			err = fmt.Errorf("OneDrive: drive id is unknown - reconnect the account")
		}
	case ProviderDropbox:
		err = oauth("dropbox")
	case ProviderBox:
		err = oauth("box")
		cfg["box_sub_type"] = "user"
		cfg["root_folder_id"] = "0"
	case ProviderPCloud:
		err = oauth("pcloud")
		host := strings.ToLower(first(c, "hostname"))
		if host == "" {
			host = "api.pcloud.com"
		}
		if host != "api.pcloud.com" && host != "eapi.pcloud.com" {
			return spec, fmt.Errorf("pCloud: unsupported API host %q", host)
		}
		cfg["hostname"] = host
	case ProviderYandex:
		err = oauth("yandex")
	case ProviderKoofr:
		spec.backend = "koofr"
		cfg["user"] = first(c, keysUser...)
		err = setObscured("password", firstRaw(c, keysPass...))
		cfg["provider"] = "koofr"
		if raw := first(c, "endpoint", "url"); raw != "" {
			if u, perr := url.Parse(raw); perr == nil && u.Host != "" && !strings.EqualFold(u.Host, "app.koofr.net") {
				cfg["provider"] = "other"
				cfg["endpoint"] = u.Scheme + "://" + u.Host
			}
		}
	case ProviderWebDAV:
		spec.backend = "webdav"
		cfg["url"] = first(c, "url")
		vendor := strings.ToLower(first(c, "vendor"))
		if vendor == "" {
			lower := strings.ToLower(cfg["url"])
			switch {
			case strings.Contains(lower, "/remote.php/dav/files/"):
				vendor = "nextcloud"
			case strings.Contains(lower, "/remote.php/webdav"):
				vendor = "owncloud"
			default:
				vendor = "other"
			}
		}
		cfg["vendor"] = vendor
		if u := first(c, keysUser...); u != "" {
			cfg["user"] = u
		}
		err = setObscured("pass", firstRaw(c, keysPass...))
		if bt := firstRaw(c, "bearer_token"); bt != "" {
			cfg["bearer_token"] = bt
		}
	case ProviderS3:
		spec.backend = "s3"
		endpoint := first(c, "endpoint")
		prov := first(c, "s3_provider")
		if prov == "" || !validS3Providers[prov] {
			prov = inferS3Provider(endpoint)
		}
		cfg["provider"] = prov
		cfg["access_key_id"] = first(c, keysS3Access...)
		cfg["secret_access_key"] = firstRaw(c, keysS3Secret...)
		cfg["env_auth"] = "false"
		region := first(c, "region")
		if region == "" {
			switch prov {
			case "Cloudflare":
				region = "auto"
			case "AWS":
				region = "us-east-1"
			}
		}
		if region != "" {
			cfg["region"] = region
		}
		if endpoint != "" {
			cfg["endpoint"] = endpoint
		}
		// Buckets are created out-of-band; bucket-scoped keys cannot HEAD/CREATE buckets.
		cfg["no_check_bucket"] = "true"
		spec.root = strings.Trim(first(c, "bucket"), "/")
		if prefix := strings.Trim(first(c, "path", "prefix"), "/"); prefix != "" {
			spec.root = path.Join(spec.root, prefix)
		}
		if spec.root == "" {
			err = fmt.Errorf("S3: bucket is required")
		}
	case ProviderMega:
		spec.backend = "mega"
		cfg["user"] = first(c, keysUser...)
		err = setObscured("pass", firstRaw(c, keysPass...))
		if secret := first(c, "otp_secret_key"); secret != "" && err == nil {
			code, terr := totpCode(secret, now)
			if terr != nil {
				return spec, fmt.Errorf("MEGA: invalid OTP secret key: %v", terr)
			}
			cfg["2fa"] = code
		} else if code := first(c, "2fa", "twofa"); code != "" {
			cfg["2fa"] = code
		}
	case ProviderFilen:
		spec.backend = "filen"
		cfg["email"] = first(c, "email", "username")
		if err = setObscured("password", firstRaw(c, "password", "pass", "filen_pass")); err == nil {
			err = setObscured("api_key", firstRaw(c, "api_key", "filen_api_key"))
		}
	case ProviderB2:
		spec.backend = "b2"
		cfg["account"] = first(c, "account", "key_id", "username")
		// b2 reads "key" verbatim (it is Sensitive, not IsPassword): never obscure it.
		cfg["key"] = firstRaw(c, "key", "application_key", "password")
		if ep := first(c, "endpoint"); ep != "" {
			cfg["endpoint"] = ep
		}
		cfg["hard_delete"] = "false"
		spec.root = strings.Trim(first(c, "bucket", "bucket_name"), "/")
		if prefix := strings.Trim(first(c, "path"), "/"); prefix != "" && spec.root != "" {
			spec.root = path.Join(spec.root, prefix)
		}
	case ProviderPikPak:
		spec.backend = "pikpak"
		cfg["user"] = first(c, "user", "username", "email")
		err = setObscured("pass", firstRaw(c, "pass", "password"))
		if id := first(c, "root_folder_id"); id != "" {
			cfg["root_folder_id"] = id
		}
		if tok := strings.TrimSpace(c["token"]); tok != "" {
			cfg["token"] = tok
		}
	case ProviderSFTP:
		spec.backend = "sftp"
		cfg["host"] = first(c, "host")
		cfg["user"] = first(c, "user", "username")
		cfg["port"] = first(c, "port")
		if cfg["port"] == "" {
			cfg["port"] = "22"
		}
		if err = setObscured("pass", firstRaw(c, "pass", "password")); err != nil {
			break
		}
		if pem := firstRaw(c, "key_pem", "private_key"); pem != "" {
			// rclone accepts either real newlines or literal "\n" sequences.
			cfg["key_pem"] = strings.ReplaceAll(strings.TrimSpace(pem), "\r\n", "\n")
		}
		if kf := first(c, "key_file"); kf != "" {
			cfg["key_file"] = kf
		}
		if err = setObscured("key_file_pass", firstRaw(c, "key_file_pass", "key_passphrase", "passphrase")); err != nil {
			break
		}
		spec.root = strings.TrimRight(first(c, "path", "root_folder"), "/")
	case ProviderSMB:
		spec.backend = "smb"
		cfg["host"] = first(c, "host")
		cfg["user"] = first(c, "user", "username")
		cfg["port"] = first(c, "port")
		if cfg["port"] == "" {
			cfg["port"] = "445"
		}
		cfg["domain"] = first(c, "domain")
		if cfg["domain"] == "" {
			cfg["domain"] = "WORKGROUP"
		}
		err = setObscured("pass", firstRaw(c, "pass", "password"))
		share := strings.Trim(first(c, "share"), "/\\")
		if share == "" {
			return spec, fmt.Errorf("SMB: share name is required")
		}
		spec.root = path.Join(share, strings.Trim(strings.ReplaceAll(first(c, "path", "root_folder"), "\\", "/"), "/"))
	case ProviderProtonDrive:
		spec.backend = "protondrive"
		cfg["username"] = first(c, "username", "user", "email")
		if err = setObscured("password", firstRaw(c, "password", "pass")); err != nil {
			break
		}
		if err = setObscured("mailbox_password", firstRaw(c, "mailbox_password")); err != nil {
			break
		}
		if secret := first(c, "otp_secret_key"); secret != "" {
			err = setObscured("otp_secret_key", strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
		} else if code := first(c, "2fa", "twofa"); code != "" {
			cfg["2fa"] = code
		}
	default:
		return spec, fmt.Errorf("unsupported provider: %s", p)
	}
	if err != nil {
		return spec, err
	}

	// Overlay state persisted by the backend itself (sessions, device ids).
	for k, v := range c {
		if strings.HasPrefix(k, stateKeyPrefix) {
			key := strings.TrimPrefix(k, stateKeyPrefix)
			if key != "" && key != "token" {
				cfg[key] = v
			}
		}
	}
	return spec, nil
}
