# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
