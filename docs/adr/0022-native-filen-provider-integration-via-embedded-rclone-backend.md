# 0022. Native Filen Provider Integration via Embedded Rclone Backend

Date: 2026-09-25

## Status

Accepted

## Context

Cloudgate aggregates multi-cloud storage services under a unified virtual filesystem. The platform previously supported Google Drive, OneDrive, Dropbox, Box, PCloud, Yandex, Koofr, Amazon S3, WebDAV, and MEGA.

Filen (`https://filen.io/`) is a privacy-first, zero-knowledge end-to-end encrypted (E2EE) cloud storage provider based in Germany. Unlike standard cloud providers:
1. **Zero Delegated OAuth 2.0**: Filen does not provide delegated OAuth2 authorization endpoints.
2. **Client-Side Zero-Knowledge Encryption**: All file payload blocks, names, and directory paths are encrypted client-side using user-derived cryptographic keys (AES-256-GCM, PBKDF2/scrypt) before leaving the local device. Servers only store ciphertext.
3. **API Key Generation Requirement**: To establish an API connection, Filen's official client architecture requires an exported master API Key (generated via the official `filen export-api-key` script/CLI) alongside the user's account email and master password.
4. **Official Native Rclone Driver**: Starting in Rclone v1.73.0, Filen is officially supported via a native Go backend (`github.com/rclone/rclone/backend/filen`).

Writing a bespoke pure-Go E2EE engine for Filen from scratch carries excessive maintenance overhead and cryptographic risk, while shelling out to external CLI binaries violates Cloudgate's single-binary static deployment model.

## Decision

We integrate Filen as a first-class `ProviderFilen` (`filen`) leveraging the embedded Rclone v1.73 backend architecture prescribed in ADR-0012:

1. **Embedded Backend via Rclone v1.73**:
   - Upgraded `github.com/rclone/rclone` to `v1.73.0` in `go.mod`.
   - Imported the native backend `_ "github.com/rclone/rclone/backend/filen"` into Cloudgate's storage layer.
   - Built a dynamic in-memory filesystem initializer using `fs.Find("filen")` with in-memory `configmap.Simple` mapping `api_key`, `email`, and `password`. No physical `rclone.conf` is written to disk.

2. **Domain Model Alignment (`CONTEXT.md` & `CONTEXT.id.md`)**:
   - Added `filen` to `Provider` enum in backend and domain vocabulary.
   - Designated the user's authenticated Filen email as the `AccountPrincipal`.
   - Categorized Filen under `DirectCredentialAuth`, reflecting the requirement of raw user credentials and master zero-knowledge API keys.

3. **In-Memory Credential Persistence (`accounts.credentials`)**:
   - Canonical credential JSON payload: `{"email": "...", "password": "...", "api_key": "..."}`.
   - Encrypted at rest using Cloudgate's `EncryptedVault` (AES-256-GCM) with master password protection.

4. **RemoteTrash & Capability Parity**:
   - Filen driver supports standard `RemoteTrash` (`/.cloudgate_trash/`) via rclone's `Move` operations.
   - Live telemetry reports accurate `About()` storage quota (`Total`, `Used`, `Free`).

5. **Guided Onboarding with Error Diagnostics**:
   - Upgraded `PROVIDER_METADATA` and `renderDocs()` in `web/dist/index.html` with step-by-step instructions.
   - Provided a one-click copy box for the official Filen API Key export script (`curl -sL https://raw.githubusercontent.com/FilenCloudDienste/filen-rs/refs/heads/main/filen-cli/export-api-key.sh | bash`).
   - Integrated targeted remediation for common failure states (invalid API key, password mismatch, two-factor authentication requirements).

6. **Hermetic Test Isolation**:
   - Configured mock fallback (`r.baseURL != ""` or `filen_api_key == "mock"`) to route file and directory operations to in-memory buffers during automated test runs (`go test ./...`).

## Consequences

- Filen users can seamlessly connect, aggregate, and browse their encrypted cloud drive within Cloudgate's `StorageHub` and `UnifiedExplorer`.
- Preserves Cloudgate's single-binary static compilation without external executable dependencies or runtime daemon bridges.
- Maintains 100% automated test suite hermeticity without requiring live external network requests during CI/CD.
