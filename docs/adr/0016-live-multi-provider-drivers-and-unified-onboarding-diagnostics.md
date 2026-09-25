# 0016. Live Multi-Provider Drivers and Unified Onboarding Diagnostics

Date: 2026-09-25

## Status

Accepted

## Context

Cloudgate aims to seamlessly aggregate heterogeneous storage providers into a single virtual filesystem. However, several critical gaps existed in previous iterations:
1. **MEGA Mock Driver**: The MEGA adapter routed file operations (`List`, `Get`, `Put`, `Delete`, `Move`, `Mkdir`) into in-memory mock structures (`memList`, `memGet`, etc.), and the onboarding flow simply simulated authentication with a client-side timer. As a result, submitted MEGA credentials never communicated with MEGA servers, leading to desynchronized local and remote state.
2. **Obsolete OneDrive OAuth & Dummy Credentials**: OneDrive used a deprecated fallback Client ID (`cloudgate-onedrive-client-id`) and unguided "leave blank if default" instructions. Modern Microsoft Entra ID (Azure) rejected these requests immediately with `AADSTS700016: Application not found`, and lacked modern Graph API scopes (`files.readwrite`, `offline_access`, `User.Read`).
3. **Production S3 & WebDAV API Bypass**: The `RcloneAdapter` contained a guard condition `if r.baseURL != ""` originally intended for test servers. In production environments where `baseURL` was empty, S3 and WebDAV requests were silently bypassed into in-memory storage instead of issuing live HTTP REST calls.
4. **Opaque Connection Failures**: When users faced authentication or network rejections, the UI provided no troubleshooting guidance, leaving users unsure how to remediate vendor-specific errors (e.g., redirect URI mismatches, missing scopes, or disabled API features).

## Decision

We implement the following technical architecture and UX remediation across the backend and frontend:

1. **Production-Grade Live MEGA Driver**:
   - Replaced all in-memory stubs in `pkg/storage/rclone_adapter.go` with live API calls using `github.com/t3rm1n4l/go-mega` (already vendor-pinned in `go.mod`).
   - Implemented `megaClient()` with session memoization, `megaAbout()` returning live storage quota, `megaList()` mapping recursive node hierarchies, and full streaming operations (`megaGet`, `megaPut`, `megaDelete`, `megaMove`, `megaMkdir`).
   - Implemented an automated mock fallback solely when offline or during mocked unit tests (`r.baseURL != "" && r.providerConfig["mega_user"] == "mock"`).

2. **Remediated S3 and WebDAV Production Routing**:
   - Updated the guard checks in `RcloneAdapter` to inspect real provider configuration attributes (`r.baseURL != "" || r.providerConfig["endpoint/bucket/url"] != ""`).
   - Real Amazon S3, MinIO, Cloudflare R2, Nextcloud, and ownCloud instances now execute live REST and WebDAV HTTP protocols.

3. **Strict, Explicit OAuth Credential Lifecycle**:
   - Removed all dummy client IDs and misleading "leave blank if default" placeholders in `pkg/auth/oauth.go` and `web/dist/index.html`.
   - Mandated custom Client ID and Client Secret input for public OAuth providers (Microsoft OneDrive, Dropbox, Box, Google Drive), properly reflecting standard self-hosted application architecture.
   - Updated OneDrive Microsoft Graph API scopes to `files.readwrite`, `offline_access`, and `User.Read`.

4. **Guided Onboarding Stepper with Provider-Specific Troubleshooting**:
   - Upgraded `PROVIDER_METADATA` in `web/dist/index.html` with explicit step-by-step developer console instructions, direct developer portal links, and one-click redirect URI copying.
   - Integrated an interactive collapsible troubleshooting matrix into each provider card, mapping explicit vendor error codes to step-by-step remediation:
     - Azure Entra ID: `AADSTS700016` (wrong Client ID), `AADSTS50011` (Redirect URI mismatch), `AADSTS7000215` (invalid Client Secret), `AADSTS9002313` (unsupported account type).
     - Google Cloud: `400 redirect_uri_mismatch`, `403 access_denied` (unapproved test user), `Drive API disabled`.
     - MEGA: `2FA requirement`, `Bad credentials`, `Mega API rate limit / -9`.
     - S3 / WebDAV: `SignatureDoesNotMatch`, `InvalidAccessKeyId`, `401 Unauthorized`, `CORS / Network Error`.

5. **Pre-Save Connection Test Verification Pipeline**:
   - Configured `executeOnboardingSave()` in the frontend to dispatch an explicit verification request to `/api/accounts/test` before persisting account configuration into SQLite.
   - Invalid or unreachable accounts are blocked from saving, displaying detailed error diagnostics and targeted remediation advice directly to the user.

6. **Centralized Cloud Setup Knowledge Base**:
   - Integrated a dedicated "Panduan Resmi Pendaftaran Akun Cloud & Troubleshooting" chapter in the in-app documentation portal (`renderDocs()`), providing searchable step-by-step documentation accessible at any time without initiating an account creation modal.

## Consequences

- Resolves silent data desynchronization for MEGA, OneDrive, S3, and WebDAV accounts.
- Replaces opaque OAuth registration rejections with crystal-clear developer setup guidance and error code remediation.
- Maintains zero external npm or CGo dependencies, preserving single-binary compilation (`go build -o bin/cloudgate`).
- Tested with automated unit test suites (`go test -count=1 ./...`) and validated end-to-end.
