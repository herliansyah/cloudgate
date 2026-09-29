# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-09-29

### Changed
- **All 16 providers now run on embedded rclone v1.73 backends** (ADR-0027): Google Drive, OneDrive, Dropbox, Box, pCloud, Yandex Disk, Koofr, S3, WebDAV and MEGA join Filen, B2, PikPak, SFTP, SMB and Proton Drive. The hand-written REST drivers and `pkg/storage/gdrive.go` were removed; pagination, chunked/resumable uploads, cross-folder moves, recursive deletes and token refresh are handled by rclone.
- **Verified onboarding**: `POST /api/accounts` validates required fields, tests the connection against the real provider and returns `422` without saving when it fails; account IDs are generated server-side; OAuth providers must use the sign-in flow.
- **Koofr** uses the native `koofr` backend with an app password; the unused Koofr OAuth flow was removed.
- **Share links**: `/api/files/share` reports `requires_auth` for gateway links; `?public=1` creates a native provider public link where supported. The share dialog explains the difference.
- **Onboarding UI & guides**: all form labels and messages are bilingual; new fields for S3 provider, WebDAV server type, MEGA/Proton OTP secret, SFTP key passphrase and SMB sub-folder; the Docs handbook is generated from the same guides as the Add Storage dialog. Guides were updated for Google Auth Platform (test users, 7-day testing tokens), Entra ID secrets, Dropbox metadata scopes, Box/Entra/Dropbox HTTPS rules, pCloud EU, Yandex `disk.info`, Nextcloud app passwords and the Filen CLI `export-api-key`.
- README, README.id and ADR-0011/0012/0016 updated to describe the actual provider architecture.
- **Dialogs**: long dialogs now scroll inside the viewport with the header and action buttons pinned, using thin themed scrollbars. The Add Storage Provider step shows the setup guide and the credential form side by side on wide screens (≥960px) and as "Setup guide / Credentials" tabs on narrow screens. The provider picker uses the same wide dialog with compact, keyboard-accessible cards (4 per row on desktop), so all 16 providers fit without scrolling.

### Fixed
- **Silent data loss**: failed uploads, moves and folder creation no longer fall back to an in-memory store that reported success; "mock" substrings in credentials no longer switch accounts to fake storage.
- **S3**: requests are signed with SigV4 using the access key, secret and region; bucket-based listing with folders, pagination, copy+delete moves and multipart uploads (R2, Wasabi, MinIO, ...).
- **Box**: correct ID-based API and upload host; rotated refresh tokens are persisted.
- **Dropbox**: requests `token_access_type=offline` so accounts keep working after 4 hours.
- **pCloud**: EU accounts (`eapi.pcloud.com`) are supported via the `hostname` returned by pCloud.
- **Backblaze B2**: the application key is no longer obscured (authentication always failed); moves work via server-side copy.
- **Proton Drive**: uploads to sub-folders no longer land in the root; the OTP secret is passed as `otp_secret_key`; the Proton session is persisted so 2FA accounts survive restarts.
- **PikPak**: `root_folder_id` is passed as an option; the account now actually signs in with email/password (rclone's config-time login step is run once), and the session token and device ID are persisted.
- **MEGA**: 2FA accounts are supported through an OTP secret; overwrites no longer delete the old file before the upload finishes.
- **WebDAV**: successful moves no longer report "file not found"; percent-encoded names are decoded.
- **SMB**: the share name is required and the sub-folder path is applied.
- **SFTP**: server host keys are pinned on first connection and verified afterwards (previously any host key was accepted).
- **Google Drive / OneDrive**: moves across folders no longer only rename the file; name-based fallbacks that could delete a different file are gone.
- **Token rotation**: refreshed OAuth tokens, OneDrive drive IDs and backend session state are written back to the database.
- **Connection tests**: S3/WebDAV no longer always succeed and Koofr no longer always fails; quotas come from the provider, and the quota you enter is only used when the provider reports none.
- **Accounts without valid credentials** are shown as unavailable instead of connected.
- **Trash, cross-account move and transfers** no longer open (and leak) a download stream just to read file metadata; on SMB this kept the file locked so moving it to trash failed. A shared `storage.Stat` helper is used instead.
- **Connection timeouts** now explain the likely cause (network/firewall or provider rate limiting) instead of a bare "context deadline exceeded".
- Guides: Box scope names, Yandex `disk.app_folder` warning, and where to find the MEGA/Proton OTP secret key.

### Security
- OAuth `state` is now a random single-use server-side nonce; client secrets no longer travel through the browser, and the login endpoint accepts `POST`.
- PKCE (S256) for Google, Microsoft and Dropbox; Google-only authorize parameters are no longer sent to other providers.
- The OAuth callback page is rendered with `html/template` (fixes reflected XSS) and posts its result only to the dashboard origin; the dashboard verifies the message origin.
- Account API responses no longer include stored credentials (passwords, tokens, keys); they stay in the database and the encrypted vault export.
- Custom `redirect_uri` values must point to the provider's Cloudgate callback path; redirect URIs behind a reverse proxy honour `X-Forwarded-Host`.

## [0.3.1] - 2026-09-28

### Added
- **Windows Executable Icon & Resource Embedding**: Added Windows PE COFF resource files (`cmd/cloudgate/rsrc_windows_amd64.syso` and `cmd/cloudgate/rsrc_windows_arm64.syso`) containing high-resolution multi-scale Cloudgate app launcher icons (up to 256x256 RGBA) and application metadata, enabling native icon display in Windows File Explorer and taskbars without external build dependencies; added `scripts/generate_winres.sh` helper and unit verification test in `cmd/cloudgate/windows_res_test.go`.

### Fixed
- **Bilingual Add Provider Modal Onboarding Guides & Metadata**: Resolved hardcoded Indonesian guides, troubleshooting instructions, input placeholders, and credential labels in the Add Storage Provider modal (`addAccountModal`) across all 16 storage providers (Google Drive, Microsoft OneDrive, Dropbox, Box, pCloud, Yandex, Mega, Filen, Backblaze B2, PikPak, SFTP/SSH, SMB/Samba, Proton Drive, S3, Nextcloud/WebDAV, and Koofr) when English (`en`) mode is active.

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
