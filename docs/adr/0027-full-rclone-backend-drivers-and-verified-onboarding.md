# 0027. Full rclone Backend Drivers, Credential Rotation and Verified Onboarding

Date: 2026-09-28

## Status

Accepted. Completes ADR-0012; supersedes the hand-written REST parts of ADR-0011 and ADR-0016.

## Context

An audit of all 16 providers found that ADR-0012 was only partly implemented:

- Only filen, b2, pikpak, sftp, smb and protondrive used embedded rclone backends. Google Drive, OneDrive, Dropbox, Box, pCloud, Yandex, S3, WebDAV and Koofr used hand-written REST calls; MEGA used go-mega directly.
- The REST code had no S3 SigV4 signing, invented Box endpoints, no pagination or chunked uploads, rename-only moves (Drive, OneDrive), hard-coded US pCloud hosts, and Dropbox never received a refresh token.
- Nearly every error path silently fell back to an in-process map, so uploads could "succeed" and disappear on restart. `isMock*` heuristics matched the substring "mock" in user credentials.
- Rotated OAuth refresh tokens (Box rotates on every refresh) and backend session state (Proton, PikPak) were never persisted.
- The OAuth `state` parameter carried the client secret in base64, had no CSRF nonce, and the callback page reflected unescaped input.
- Accounts were saved as "connected" without verifying credentials.

## Decision

1. **Every provider is an rclone backend.** `RcloneAdapter` wraps a real `fs.Fs` created through the rclone registry (`fs.Find(name).NewFs`) for `drive, onedrive, dropbox, box, pcloud, yandex, koofr, s3, webdav, mega, filen, b2, pikpak, sftp, smb, protondrive`. `gdrive.go` is removed. Driver operations are implemented once on top of `fs.Fs` (`List`, `NewObject/Open`, `Put`/`Update`/`PutStream`, `operations.Move`, `DirMove`, `operations.Purge`, `Features().About`).
2. **Config mapping is explicit and tested.** `buildBackendConfig` maps CloudGate credential keys to rclone options, obscuring exactly the options rclone reveals (never B2 `key`), passing buckets/shares as the Fs root, Proton `otp_secret_key` as its own option, PikPak `root_folder_id` as an option, and rclone option defaults through a `configmap.Mapper`.
3. **No silent fallback.** Real errors are returned. Accounts that cannot be restored get an `UnavailableDriver`. rclone's `memory` backend is used only when Go code passes `WithMemoryBackend()` (tests), never from user data.
4. **Credential rotation is persisted.** The mapper's `Set` (called by rclone for token refresh and session state) updates the credentials and a `CredentialPersister` writes them to `accounts.credentials`. Backend state is stored under the `rclone.` key prefix.
5. **Bootstrap steps rclone normally does in `rclone config`:** OneDrive `drive_id`/`drive_type` discovery, pCloud API host from the OAuth redirect (`hostname`), SFTP host-key pinning on first use (`known_host_key` → runtime `known_hosts_file`), MEGA TOTP generation from an OTP secret.
6. **OAuth hardening.** Server-side single-use state nonces (15 min TTL) hold client secret, redirect URI and PKCE verifier; PKCE S256 for Google, Microsoft and Dropbox; Dropbox `token_access_type=offline`; Google-only `access_type`/`prompt`; Koofr OAuth removed; `POST /api/auth/{provider}/login`; `html/template` callback page with `postMessage` targeted at the dashboard origin.
7. **Verified onboarding.** `POST /api/accounts` validates required fields, runs a live connection test and returns `422` without saving when the provider rejects the credentials. Account IDs are generated server-side.

## Consequences

- Behaviour now matches rclone's mature implementations (pagination, resumable/chunked uploads, retries, encoding, trash semantics).
- The binary grows because more rclone backends (notably the AWS SDK for S3) are linked in.
- Existing OAuth accounts keep working when their refresh token is still valid (legacy `access_token`/`refresh_token` are converted to an rclone token and refreshed on first use). Box accounts whose rotated refresh token was discarded before this change, Dropbox accounts without a refresh token, and accounts with missing credentials must be reconnected; they are shown with an error instead of pretending to be connected.
- Tests exercise the generic driver with the rclone memory backend and the provider mapping with unit tests; live provider behaviour still depends on each provider's API.
