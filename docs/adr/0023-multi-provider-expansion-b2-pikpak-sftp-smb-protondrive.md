# 0023. Multi-Provider Expansion via Embedded Rclone Backends (B2, PikPak, SFTP, SMB, Proton Drive)

Date: 2026-09-26

## Status

Accepted

## Context

Cloudgate provides unified virtual storage aggregation across cloud services and protocols. To date, Cloudgate supported Google Drive, OneDrive, Dropbox, Box, PCloud, Yandex, Koofr, Amazon S3, WebDAV, MEGA, and Filen.

Users frequently request support for additional popular cloud services (such as Backblaze B2, PikPak, and Proton Drive) as well as standard server/NAS protocols (SFTP and SMB).

Rather than supporting unofficial, brittle scraper mechanisms (e.g. TeraBox session cookies) or requiring users to run external daemon bridges or custom CLI binaries, upstream `github.com/rclone/rclone v1.73.0` already includes battle-tested Go backend packages for:
1. **Backblaze B2** (`backend/b2`)
2. **PikPak** (`backend/pikpak`)
3. **SFTP** (`backend/sftp`)
4. **SMB** (`backend/smb`)
5. **Proton Drive** (`backend/protondrive`)

## Decision

We adopt a phased, step-by-step rollout (following Matt Pocock's wayfinding and issue-tracking conventions in `.scratch/multi-provider-expansion/`):
1. **Sequence**: Backblaze B2 (`01`) → PikPak (`02`) → SFTP (`03`) → SMB (`04`) → Proton Drive (`05`).
2. **Embedded Rclone Backend Architecture**:
   - Embed respective backend packages into Cloudgate (`pkg/storage/rclone_adapter.go`).
   - Dynamically initialize filesystems in-memory with `fs.Find(...)` and `configmap.Simple`.
   - Never write sensitive configuration files (`rclone.conf`) to disk.
3. **Domain Model & Credential Persistence**:
   - All 5 providers are governed under `DirectCredentialAuth`.
   - Sensitive credentials are encrypted at rest with `EncryptedVault` (AES-256-GCM) in the `accounts.credentials` SQLite column.
   - Canonical `AccountPrincipal` formatting:
     - B2: `key_id` or `bucket_name`
     - PikPak: user email/username
     - SFTP: `username@host[:port]`
     - SMB: `username@host/share`
     - Proton Drive: Proton email address
4. **Hermetic Test Suite**:
   - All unit and integration test routines (`go test ./...`) run against isolated in-memory test doubles without requiring live external network requests.

## Consequences

- Users gain native single-binary access to 5 major cloud storage providers and network protocols.
- Zero external executable requirements; preserving Cloudgate's portable single-binary model.
- Incremental verification guarantees stable delivery without breaking existing providers.
