# 0026. Automated Self-Update Lifecycle and Embedded Changelog

Date: 2026-09-27

## Status

Accepted

## Context

Cloudgate is distributed as a single self-contained Go binary. While basic version querying against GitHub Releases was introduced in early iterations (`pkg/updater/`), the user had no mechanism to apply updates within the application, verify binary integrity before execution, or view version changes offline.

Deploying updates to long-running server daemons introduces critical architectural concerns:
1. **Security & Trust Boundary**: Downloading and executing arbitrary binaries from the internet without cryptographic validation creates severe tampering risks.
2. **Process Lifecycle**: Replacing a running executable on disk does not upgrade in-memory instructions; terminating without graceful shutdown drops active file transfers and keeps the `InstanceLock` file locked, preventing immediate re-execution.
3. **API Rate Limiting**: Repeatedly querying the unauthenticated GitHub Releases API on page loads risks exhausting the 60 requests/hour IP limit.
4. **Offline Changelog Visibility**: Users operating in private or local network environments need to review what changed across versions without requiring an active internet connection.

## Decision

We establish an end-to-end self-update lifecycle and embedded changelog architecture:

1. **Strict SHA-256 ReleasePackage Verification**:
   - The updater locates official binary assets alongside `checksums.txt` published on GitHub Releases.
   - Before executing atomic binary replacement (`os.Rename`), the updater calculates the SHA-256 hash of the downloaded payload and strictly validates it against `checksums.txt`. If the checksum asset is absent or the hash mismatches, the update is immediately rejected.

2. **Graceful ProcessRestart Transition**:
   - Applying an update shuts down HTTP listeners and clears the `InstanceLock` file.
   - On Unix/Linux systems, the running process re-executes itself in-place via `syscall.Exec(executablePath, os.Args, os.Environ())`, preserving command-line arguments and port bindings without changing the process ID.
   - On non-Unix environments (Windows), a clean process exit is triggered with instructions for the service manager or user.
   - The Web UI displays a reconnection overlay that polls the server until the updated version comes back online.

3. **Background Check & Rate-Limit Caching**:
   - Version checking runs asynchronously upon server startup and on a 24-hour periodic schedule.
   - Results are cached in memory; `GET /api/updater/check` serves cached data with an optional `force=true` query parameter for manual refreshes.

4. **Embedded Offline ReleaseChangelog & Remote Notes**:
   - `CHANGELOG.md` is embedded into the executable, accessible offline via `GET /api/changelog`, a dedicated modal dialog in the Web UI, and the CLI command `cloudgate changelog`.
   - When a newer `ReleasePackage` is detected, the GitHub Release release notes (`rel.Body`) are presented directly in the update confirmation prompt.

5. **Access Control via GatewayAuth**:
   - Triggering binary updates via `POST /api/updater/apply` is strictly guarded by `GatewayAuth` (`MasterPassword`), preventing unauthenticated remote clients from modifying binary assets or initiating restarts.

## Consequences

- Users gain a seamless, single-click update experience from the Web UI and a one-command workflow via `cloudgate update`.
- Binary authenticity and integrity are cryptographically guaranteed before any disk mutation occurs.
- Upgrades do not leave stale locks or dangling sockets.
- Release change history is always accessible directly inside the binary offline.
