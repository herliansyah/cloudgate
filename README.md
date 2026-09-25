# Cloudgate

[English](README.md) | [Bahasa Indonesia](README.id.md)

**Cloudgate** is a lightweight, unified cloud storage gateway and multi-account aggregator compiled into a single static Go binary with an embedded Google Drive-inspired dark-mode Web UI and command-line interface.

- **Author**: Herliansyah
- **Repository**: [https://github.com/herliansyah/cloudgate](https://github.com/herliansyah/cloudgate)
- **License**: MIT

---

## Key Features

1. **Multi-Account Aggregator**: Connect multiple personal and work cloud accounts across providers (Google Drive, OneDrive, Dropbox, Box, Mega, pCloud, Nextcloud/WebDAV, S3, Koofr, Yandex Disk).
2. **Capacity-Aware StoragePool**: Combine multiple storage accounts into a unified virtual pool where incoming writes are automatically distributed using capacity-aware round-robin without splitting intact files.
3. **Zero-Disk Streaming Transfers**: Perform direct cross-account file copy and move operations via in-memory `io.Pipe` streaming with zero temporary host disk footprint and verified safe-move semantics.
4. **Isolated Remote Trash**: Soft-delete files into an isolated remote directory (`/.cloudgate_trash/`) with a local SQLite catalog mapping original paths for deterministic one-click restoration.
5. **Local SQLite Architecture**: Powered by pure-Go SQLite (`modernc.org/sqlite`) for zero-CGO static compilation, featuring an FTS5 full-text search index and an atomic 100-event rolling audit log.
6. **Encrypted Vault & GitHub Sync**: Secure account configurations and credentials with PBKDF2 + AES-256-GCM encryption, with optional synchronization to private GitHub repositories.
7. **Single-Instance Mutex & Port Hunting**: Automatically acquires an OS lock file (`~/.config/cloudgate/cloudgate.lock`) to prevent duplicate processes, and scans available ports starting at `5210` (`5210..5300`) listening on `0.0.0.0` for local and LAN access.
8. **Self-Updating**: Built-in GitHub Releases updater checks for official releases and verifies SHA-256 checksums.

---

## Quick Start

### Build from Source

```bash
# Clone the repository
git clone https://github.com/herliansyah/cloudgate.git
cd cloudgate

# Build single static binary with embedded web assets
go build -o bin/cloudgate cmd/cloudgate/main.go
```

### Run Server

```bash
# Start gateway server (defaults to 0.0.0.0:5210)
./bin/cloudgate serve

# Custom host or starting port
./bin/cloudgate serve --host 0.0.0.0 --port 5210
```

Access the Web UI in your browser:
- Local: `http://localhost:5210` (or `http://127.0.0.1:5210`)
- LAN / Other Devices: `http://<your-lan-ip>:5210`

---

## Google Drive Integration Guide

To connect a personal or corporate Google Drive account:

### 1. Enable Google Drive API (Mandatory)
In your Google Cloud project, the Google Drive API must be enabled:
- Visit [Google Drive API Overview](https://console.developers.google.com/apis/api/drive.googleapis.com/overview).
- Click **"ENABLE"** (Aktifkan).

### 2. Configure OAuth Consent Screen
- Go to [Google Cloud Console - OAuth Consent Screen](https://console.cloud.google.com/apis/credentials/consent).
- If your publishing status is **Testing**, add your Google email address under **Test users**.

### 3. Create OAuth 2.0 Credentials
- Go to [Google Cloud Console - Credentials](https://console.cloud.google.com/apis/credentials).
- Click **Create Credentials** &rarr; **OAuth client ID**.
- Select **Web application** (or **Desktop app**).
- Under **Authorized redirect URIs**, add:
  ```
  http://localhost:5210/api/auth/google/callback
  ```
- Copy the generated **Client ID** and **Client Secret**.

### 4. Connect in Cloudgate
- In the Cloudgate Web UI, click **"+ Add Account"**.
- Select **Google Drive**.
- Paste your **Client ID** and **Client Secret**.
- Click **"Masuk dengan Akun Google"** and approve access.
- Cloudgate will complete the token exchange, fetch your account details, read your real storage quota, and list your files.

---

## CLI Reference

```
cloudgate                  Start server on 0.0.0.0:5210 and open Web UI
cloudgate serve            Start server in current terminal
  --host string            Host IP to bind (default "0.0.0.0")
  --port int               Starting port (default 5210, auto-hunts if occupied)
cloudgate accounts         List connected cloud storage accounts
cloudgate audit            View recent 100 audit events
cloudgate auth status     Check GatewayAuth protection status
cloudgate auth setup <pw>  Set initial MasterPassword from terminal
cloudgate auth reset       Reset MasterPassword and return to setup state
cloudgate version          Show version and author information
cloudgate help             Show command help
```

---

## Architecture & Codebase Layout

```
.
├── cmd/cloudgate/         # Main binary entrypoint and CLI commands
├── pkg/
│   ├── auth/              # OAuth2 consent URL generation, token exchange, & bcrypt MasterPassword
│   ├── config/            # Config path resolver & single-instance lockfile
│   ├── db/                # Pure-Go SQLite schema, migrations, & FTS5 search
│   ├── server/            # REST API, static asset server, & port hunting
│   ├── storage/           # Vendor drivers (GDriveDriver), StoragePool, Trash, Transfers
│   ├── sync/              # Encrypted vault GitHub synchronization
│   ├── updater/           # GitHub Releases version checking & updates
│   └── vault/             # PBKDF2 + AES-256-GCM encrypted vault
├── web/                   # Embedded SPA frontend (Google Drive dark-mode UI)
├── docs/
│   ├── adr/               # Architectural Decision Records (0001 - 0021)
│   └── agents/            # Domain conventions and agent triage specifications
├── CONTEXT.md             # Canonical ubiquitous language and domain glossary
└── AGENTS.md              # Agent behavioral rules and skill map
```

---

## Testing

Run the full automated test suite:

```bash
go test -v ./pkg/...
```
