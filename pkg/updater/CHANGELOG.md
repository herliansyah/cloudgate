# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Windows Executable Icon & Resource Embedding**: Added Windows PE COFF resource files (`cmd/cloudgate/rsrc_windows_amd64.syso` and `cmd/cloudgate/rsrc_windows_arm64.syso`) containing high-resolution multi-scale Cloudgate app launcher icons (up to 256x256 RGBA) and application metadata, enabling native icon display in Windows File Explorer and taskbars without external build dependencies; added `scripts/generate_winres.sh` helper and unit verification test in `cmd/cloudgate/windows_res_test.go`.

## [0.3.0] - 2026-09-28

### Added
- **Visual Identity, Vector Branding & Embedded Favicon**: Designed and introduced official Cloudgate branding assets featuring an electric cyber-indigo gradient cloud integrated with an ingress portal and data beam. Added scalable vector marks (`docs/assets/logo.svg`, `docs/assets/logo-horizontal.svg`), squircle app launcher icon (`docs/assets/app-icon.svg`, `docs/assets/app-icon.png`), dual favicon assets (`web/dist/favicon.svg`, `web/dist/favicon.ico`), upgraded embedded Web UI navbar brand mark, and integrated high-contrast horizontal logo banners across `README.md` and `README.id.md`.

### Fixed
- **Bilingual Localization for Provider Handbook, Starred & Recent Views**: Resolved hardcoded Indonesian text in the 16-provider registration & troubleshooting handbook (`renderHandbookCards`), dynamic empty states for Starred (`starred.emptyTitle`, `starred.emptyDesc`, `starred.explore`) and Recent views (`recent.emptyTitle`, `recent.emptyDesc`, `recent.explore`), star/unstar toasts, StorageHub sync & testing notifications, account editing & disconnect alerts, task management actions, and OAuth sign-in controls when English (`en`) mode is active.

## [0.2.3] - 2026-09-28

### Fixed
- **Bilingual Consistency & English UI Coverage**: Resolved hardcoded Indonesian strings across file explorer empty states, category filters, breadcrumb navigation, context menus, side sheet inspector, StorageHub table headers, URL Ingest modal, Folder Sync modal, task status drawer, and REST API documentation. Expanded client-side `I18N` dictionaries with ~90 keys and enhanced `applyTranslations()` for seamless English and Indonesian language switching.

### Changed
- **CLI Startup Banner Framing**: Refined Cyber Block ASCII art banner styling with High-Intensity ANSI Cyan (`\033[1;36m`), 2-space left padding, top spacing, and 75-column divider bars for balanced terminal framing.

## [0.2.2] - 2026-09-28

### Added
- **Matrix-Style CLI Startup Banner**: Introduced Cyber Block ASCII art banner with High-Intensity ANSI Green styling and zero-dependency terminal capability and fallback detection (`NO_COLOR`, non-TTY, and `TERM=dumb`).

### Changed
- **CLI Language Standardization**: Standardized CLI startup banners, flags, and operational prompts (`cmd/cloudgate`) to pure English, aligning with Unix command conventions while preserving bilingual Web UI capabilities.

## [0.2.1] - 2026-09-28

### Changed
- **StoragePool UI Branding & Canonical Terminology**: Replaced generic "All Files" / "Semua File" navigation labels with the canonical domain term `StoragePool` across sidebar navigation, breadcrumbs, and hero action buttons in both English and Indonesian modes; updated `UnifiedExplorer` domain definition in `CONTEXT.md` and `CONTEXT.id.md`.

### Fixed
- **Web UI HTML Escaping (`escapeHtml`)**: Defined missing client-side `escapeHtml` utility in embedded `index.html` preventing `ReferenceError: escapeHtml is not defined` during updater checks, task options population, and task status drawer rendering.

## [0.2.0] - 2026-09-28

### Added
- **Automated Self-Update Engine (`pkg/updater`)**: End-to-end in-place binary upgrade mechanism with strict SHA-256 integrity verification against official `checksums.txt` assets on GitHub Releases.
- **Automated CI/CD Release Pipeline**: GitHub Actions workflow (`.github/workflows/release.yml`) compiling multi-platform standalone binaries, generating SHA-256 `checksums.txt`, extracting release notes, and publishing GitHub Releases on tag push.
- **Docker Containerization & GHCR Releases**: Multi-stage `Dockerfile`, `docker-compose.yml`, and automated multi-architecture Docker image builds (`linux/amd64` & `linux/arm64`) published to GitHub Container Registry (`ghcr.io/herliansyah/cloudgate`) on every release.
- **Docker Deployment Documentation**: Detailed quick-start guides for Docker Run and Docker Compose with volume mounting and remote GatewayAuth configuration in `README.md` and `README.id.md`.
- **Cross-Platform Process Lock**: Windows-compatible instance locking implementation using `golang.org/x/sys/windows` alongside Unix `syscall.Flock`.
- **Interactive Remote Push in Release Script**: Enhanced `scripts/release.sh` with optional interactive prompt to push commits and tags to remote origin immediately after gating passes.
- **ProcessRestart Lifecycle**: In-process graceful socket teardown, `InstanceLock` release, and atomic `syscall.Exec` re-execution on Unix/Linux with automatic Web UI reconnect polling.
- **Embedded ReleaseChangelog**: In-app offline changelog viewing via `GET /api/changelog`, dedicated Material Design 3 changelog dialog in the Web UI, and `cloudgate changelog` CLI command.
- **CLI Self-Update (`cloudgate update`)**: Interactive terminal command with release notes preview, SHA-256 verification, and `-y`/`--yes` non-interactive flag.
- **Background Release Telemetry**: Asynchronous 24-hour rate-limit caching for GitHub Releases checks with topbar notification indicator in the Web UI.
- **GatewayAuth Protection on Self-Update**: `POST /api/updater/apply` endpoint secured behind MasterPassword authentication to prevent unauthorized tampering or denial of service.
- **Provider Catalog REST API (`GET /api/providers`)**: Added endpoint exposing all 16 supported cloud and server protocol storage providers with metadata, authentication mechanisms, and categories.

### Fixed
- **Web UI Version Parity**: Synchronized hardcoded static web header badge and About modal version from stale `0.2.0` to match `config.AppVersion` (`0.1.0`), and added dynamic `/api/info` synchronization on page initialization.
- **Provider Documentation & Setup Sync in Web UI**: Synchronized the "Pengaturan & API" documentation screen to include step-by-step guides and troubleshooting cards for all 16 supported providers (adding Box, pCloud, Yandex Disk, Koofr, Backblaze B2, PikPak, SFTP, SMB, and Proton Drive), replaced static port 8080 redirect URIs with dynamic host origin, and expanded the REST API reference.

## [0.1.0] - 2026-09-27

### Added
- **Multi-Provider Cloud Storage Support**: Drivers and embedded rclone adapters for 12+ cloud and protocol providers including Google Drive, OneDrive, Dropbox, Mega, Filen, Proton Drive, PikPak, AWS S3, Backblaze B2, SFTP, SMB, WebDAV, and Local disk.
- **Unified Cloud Storage Hub**: Abstract storage interface (`StorageDriver`) with unified file operations, capacity tracking, and account pooling.
- **AES-256-GCM Encrypted Vault**: Secure local credential vault with master-key derivation (PBKDF2/Argon2) and optional encrypted GitHub synchronization.
- **Pure-Go SQLite Catalog**: Embedded database indexing file metadata with SQLite FTS5 full-text search, starred bookmarks, and trash management without external database dependencies.
- **Isolated Remote Trash**: Soft-delete safety mechanism routing deleted files to provider-isolated `/.cloudgate_trash/` folders with one-click catalog restoration.
- **Asynchronous Storage Tasks**: Background worker engine with persistent task catalog (`storage_tasks`) supporting recursive folder transfers, one-way replication (additive & mirror), and direct remote HTTP/HTTPS streaming ingest with SSRF protections.
- **Task Drawer UI**: Real-time floating task viewport with live telemetry progress bars, error indicators, and task cancellation controls.
- **GatewayAuth & MasterPassword**: Mandatory local authentication barrier for web dashboard and REST API endpoints with loopback-restricted first-run initialization.
- **Modern Responsive Web UI**: Glassmorphism and Material Design 3 UI with dark/light themes, bilingual toggle (English & Indonesian), and mobile-responsive layout.
- **Self-Update Engine**: Automated version check against official GitHub Releases with in-place binary update capabilities.
- **Rolling Audit Log**: Bounded 100-event rolling audit trail tracking user and administrative file manipulations.
- **Process Mutex**: Single-instance filesystem lock protecting configuration files and SQLite databases from concurrent execution.
