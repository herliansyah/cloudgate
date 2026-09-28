<div align="center">

<img src="docs/assets/logo-horizontal.svg" alt="Cloudgate Logo" width="380">

### Lightweight Unified Cloud Storage Gateway & Multi-Account Aggregator

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/badge/Release-v0.1.0-blue.svg)](https://github.com/herliansyah/cloudgate/releases)
[![Pure-Go](https://img.shields.io/badge/Pure--Go-Zero--CGO-success.svg)](#architecture--codebase-layout)
[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)](#testing)

**A single static Go binary with an embedded Google Drive-inspired dark-mode Web UI, REST API, and CLI.**

[Key Features](#key-features) • [Supported Providers](#supported-providers) • [Quick Start](#quick-start) • [Architecture](#architecture--codebase-layout) • [Author](#author--maintainer) • [License](#license)

[English](README.md) | [Bahasa Indonesia](README.id.md)

</div>

---

## Key Features

1. **Multi-Account Aggregator**: Connect multiple personal, team, and server accounts across 16 supported providers and protocols:
   - **Cloud Drives**: Google Drive, Microsoft OneDrive, Dropbox, Box, pCloud, Yandex Disk, Koofr
   - **Privacy & Encrypted Clouds**: MEGA, Filen, Proton Drive, PikPak
   - **Object Storage & Server Protocols**: Amazon S3, Backblaze B2, Nextcloud / WebDAV, SFTP, SMB (Windows Share / Samba)
2. **Capacity-Aware StoragePool**: Combine multiple storage accounts into a unified virtual pool where incoming writes are automatically distributed using capacity-aware round-robin without splitting intact files.
3. **Async Background Task Queue & Folder Transfers**: Persistent 2-worker FIFO queue in SQLite for long-running file transfers and recursive directory trees with restart recovery and cancellation.
4. **Direct Remote Ingest (URL Download to Cloud)**: Zero-disk streaming pipeline fetching web resources (HTTP/HTTPS) directly into any cloud drive with strict SSRF network protection.
5. **Folder Replication & Sync**: Cross-account directory synchronization with additive and mirror modes (orphans soft-deleted into isolated `RemoteTrash`).
6. **Zero-Disk Streaming Transfers**: Perform direct cross-account file copy and move operations via in-memory `io.Pipe` streaming with zero temporary host disk footprint and verified safe-move semantics.
7. **Isolated Remote Trash**: Soft-delete files into an isolated remote directory (`/.cloudgate_trash/`) with a local SQLite catalog mapping original paths for deterministic one-click restoration.
8. **Local SQLite Architecture**: Powered by pure-Go SQLite (`modernc.org/sqlite`) for zero-CGO static compilation, featuring an FTS5 full-text search index, bounded 100-record task rolling window, and an atomic 100-event rolling audit log.
9. **Encrypted Vault & GitHub Sync**: Secure account configurations and credentials with PBKDF2 + AES-256-GCM encryption, with optional synchronization to private GitHub repositories.
10. **Single-Instance Mutex & Port Hunting**: Automatically acquires an OS lock file (`~/.config/cloudgate/cloudgate.lock`) to prevent duplicate processes, and scans available ports starting at `5210` (`5210..5300`) listening on `0.0.0.0` for local and LAN access.
11. **Automated Self-Update & Offline Changelog**: Built-in GitHub Releases updater checks for official releases, strictly verifies SHA-256 checksums against `checksums.txt`, applies in-place binary upgrades, and performs graceful in-process restarts (`ProcessRestart`) with embedded offline changelog viewing (`cloudgate changelog` and `cloudgate update`).
12. **Bilingual UI & Documentation**: Seamless instant toggle between English and Bahasa Indonesia with persisted preferences and synchronized bilingual documentation.

---

## Supported Providers

Cloudgate connects to **16 storage providers and protocols** through an embedded rclone engine without requiring external daemon bridges:

| Provider | Category | Auth Method | Zero-Disk Streaming | Setup Friction |
| :--- | :--- | :--- | :---: | :--- |
| **Google Drive** | Cloud Drive | OAuth 2.0 | Yes (`io.Pipe`) | Client ID / Secret ([Guide](#google-drive-integration-guide)) |
| **Microsoft OneDrive** | Cloud Drive | OAuth 2.0 | Yes (`io.Pipe`) | Browser Consent |
| **Dropbox** | Cloud Drive | OAuth 2.0 | Yes (`io.Pipe`) | Browser Consent |
| **Box** | Cloud Drive | OAuth 2.0 | Yes (`io.Pipe`) | Browser Consent |
| **pCloud** | Cloud Drive | OAuth 2.0 | Yes (`io.Pipe`) | Browser Consent |
| **Yandex Disk** | Cloud Drive | OAuth 2.0 | Yes (`io.Pipe`) | Browser Consent |
| **Koofr** | Cloud Drive | Direct Credentials | Yes (`io.Pipe`) | User & Password |
| **MEGA** | Privacy Cloud | Direct Credentials | Yes (`io.Pipe`) | Email & Password |
| **Filen** | Privacy Cloud | Direct Credentials | Yes (`io.Pipe`) | Email, Password & 2FA |
| **Proton Drive** | Privacy Cloud | Direct Credentials | Yes (`io.Pipe`) | Username, Password & 2FA |
| **PikPak** | Privacy Cloud | Direct Credentials | Yes (`io.Pipe`) | Email & Password |
| **Amazon S3** | Object Storage | Access Keys | Yes (`io.Pipe`) | Key, Secret, Endpoint, Bucket |
| **Backblaze B2** | Object Storage | Access Keys | Yes (`io.Pipe`) | Key ID, App Key, Bucket |
| **Nextcloud / WebDAV** | Protocol / Cloud | Direct Credentials | Yes (`io.Pipe`) | URL, Username, Password |
| **SFTP** | Server Protocol | SSH Credentials | Yes (`io.Pipe`) | Host, Port, User, Password / Key |
| **SMB (Samba / Windows)** | Server Protocol | Network Share | Yes (`io.Pipe`) | Host, Share, User, Password |

<!-- Web UI Preview Placeholder (e.g. docs/assets/cloudgate-preview.png) -->

---

## Quick Start

### 1. Download Pre-compiled Binary (Recommended)

Download the latest static binary for your operating system and architecture from [GitHub Releases](https://github.com/herliansyah/cloudgate/releases):

```bash
# Example for Linux (make executable and run)
chmod +x cloudgate
./cloudgate serve
```

### 2. Or Build from Source

Requirements: Go 1.22+

```bash
# Clone the repository
git clone https://github.com/herliansyah/cloudgate.git
cd cloudgate

# Build single static binary with embedded web assets
go build -o bin/cloudgate cmd/cloudgate/main.go
```

### 3. Or Run with Docker / Docker Compose

Cloudgate is packaged as a minimal, secure multi-architecture container image (`linux/amd64`, `linux/arm64`) on GitHub Container Registry:

#### Using Docker Run:
```bash
docker run -d \
  --name cloudgate \
  --restart unless-stopped \
  -p 5210:5210 \
  -v $(pwd)/data:/data \
  ghcr.io/herliansyah/cloudgate:latest
```

#### Using Docker Compose:
```yaml
services:
  cloudgate:
    image: ghcr.io/herliansyah/cloudgate:latest
    container_name: cloudgate
    restart: unless-stopped
    ports:
      - "5210:5210"
    volumes:
      - ./data:/data
    environment:
      - CLOUDGATE_CONFIG_DIR=/data
```
Run:
```bash
docker compose up -d
```

> [!TIP]
> **Headless / Remote Docker Setup**:
> If deploying on a remote VPS or headless Docker host, initialize your MasterPassword via the container CLI:
> ```bash
> docker exec -it cloudgate cloudgate auth setup "your-secure-master-password"
> ```

### 4. Run Server (Native Binary)

```bash
# Start gateway server (defaults to 0.0.0.0:5210)
./bin/cloudgate serve

# Custom host or starting port
./bin/cloudgate serve --host 0.0.0.0 --port 5210
```

Access the Web UI in your browser:
- Local: `http://localhost:5210` (or `http://127.0.0.1:5210`)
- LAN / Other Devices: `http://<your-lan-ip>:5210`

> [!IMPORTANT]
> **First-Run GatewayAuth Security (MasterPassword)**:
> Cloudgate enforces a mandatory administrative `MasterPassword` on first launch. For security, initializing the password via the Web UI is strictly restricted to loopback (`localhost` / `127.0.0.1`).
> If deploying on a headless server or remote VPS, configure your password via the CLI first before accessing the Web UI remotely:
> ```bash
> ./bin/cloudgate auth setup "your-secure-master-password"
> ```

### 5. CLI Utilities & Self-Update

```bash
# Check and apply latest version update from GitHub Releases
./bin/cloudgate update

# Apply update automatically without interactive prompt
./bin/cloudgate update -y

# Read embedded human-readable release changelog offline
./bin/cloudgate changelog

# Inspect GatewayAuth status or reset password
./bin/cloudgate auth status
./bin/cloudgate auth reset
```

---

## Connecting Cloud Providers

Cloudgate connects to providers via two authentication methods:

1. **Direct Credential & Server Protocol Providers** (Instant Setup):
   - **Supported**: MEGA, Filen, Proton Drive, PikPak, Amazon S3, Backblaze B2, Nextcloud / WebDAV, SFTP, SMB (Windows Share / Samba).
   - **How to connect**: In the Web UI, click **"+ Add Account"**, select the provider, and enter your login credentials, API key, or server address directly. No external developer registration is required.
2. **OAuth Delegated Providers** (App Consent):
   - **Supported**: Google Drive, Microsoft OneDrive, Dropbox, Box.
   - **How to connect**: Requires standard OAuth Client ID & Secret credentials. Follow the step-by-step walkthrough below for Google Drive as a reference.

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
│   ├── adr/               # Architectural Decision Records (0001 - 0024)
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

---

## Author & Maintainer

Created with ❤️ by **Herliansyah**
- **GitHub**: [@herliansyah](https://github.com/herliansyah)
- **Repository**: [https://github.com/herliansyah/cloudgate](https://github.com/herliansyah/cloudgate)

Contributions, feature suggestions, and bug reports are warmly welcome!

---

## License

This project is licensed under the [MIT License](LICENSE).
Copyright &copy; 2026 Herliansyah.

