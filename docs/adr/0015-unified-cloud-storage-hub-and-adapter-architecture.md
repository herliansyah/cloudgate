# 0015. Unified Cloud Storage Hub and Adapter Architecture

Date: 2026-09-25

## Status

Accepted

## Context

Cloudgate previously had dual representations of connected cloud accounts: `RemoteAccount` entries were listed both in the persistent sidebar drawer and within the primary Dashboard grid. Furthermore, account connection flows utilized basic unguided HTML forms, and cross-account views like Starred, Recent files, and account-level lifecycle controls (such as temporarily pausing write distribution without severing authentication) were absent.

The user required a unified cloud storage aggregator experience that feels like a single cohesive storage system rather than disconnected vendor dashboards, while strictly adhering to Cloudgate's zero-dependency, single-binary architecture (`go build -o bin/cloudgate`).

## Decision

We establish the following architectural and interaction model:

1. **StorageHub as the Dedicated Account Command Center**:
   - The primary navigation sidebar is streamlined into distinct, non-overlapping destinations:
     - **Dashboard**: High-level overview, global storage metrics, upload dropzone, recent files, and recent audit activity.
     - **UnifiedExplorer (All Files)**: Single consolidated virtual file system aggregating all enabled accounts.
     - **Starred**: Instant access to user-flagged files across all accounts stored in SQLite.
     - **Recent**: Chronologically sorted list of recently accessed/modified files.
     - **Trash Bin**: Soft-deleted files catalog with restoration capabilities.
     - **StorageHub**: Centralized management showing total/used/free storage, multi-provider quota distribution bar, account fleet table/cards, manual reconciliation (`Sync Now`), and connection of new accounts (`Add Storage`).
     - **Settings & API Docs**: System configuration and interactive REST API specification.
   - Removed raw duplicate account lists from the sidebar to eliminate visual redundancy.

2. **ConnectionPipeline (5-Stage Onboarding Flow)**:
   - Replaced unguided modal forms with a 5-phase structured onboarding sequence:
     1. `Select Provider`: Visual grid of cloud providers with brand marks and protocol badges.
     2. `Authenticate & Configure`: Intuitive credentials entry (OAuth 2.0 handshake or direct access keys) with quota and mount settings.
     3. `Test Connection`: Automated diagnostic ping verifying network reachability and credentials validity.
     4. `Fetch Storage Information`: Live interrogation of remote capacity and current utilization.
     5. `Save Account`: Secure local persistence of credentials in SQLite and immediate live registration into active StoragePools.

3. **Standardized Provider Adapter Contract**:
   - The Go `storage.Driver` interface is strengthened as the sole boundary between Cloudgate's core engine and cloud providers, exposing:
     `ID`, `Provider`, `About`, `List`, `Get`, `Put`, `Delete`, `Move`, `Mkdir`, `TestConnection`, `GetStorageInfo`, and `GetShareLink`.
   - Business logic, HTTP routing, and SQLite storage pools interact solely with this uniform adapter contract, preventing vendor-specific logic from leaking into handlers or the UI.

4. **AccountIntegrationState & SyncSession**:
   - Added an `enabled` state flag to `RemoteAccount` allowing users to toggle accounts active or paused for round-robin writes without revoking credentials.
   - Introduced a `POST /api/storage/sync` endpoint (`Sync Now`) to refresh quota telemetry and reconcile remote state across all connected accounts.

5. **Local Catalog Extensions (Starred & Recent)**:
   - Added a `starred_files` table in SQLite for instant cross-account bookmarked files.
   - Implemented `/api/files/recent` and `/api/files/starred` endpoints.

## Consequences

- Completely eliminates visual redundancy between sidebar navigation and the central workspace.
- Provides a clean, modern, professional enterprise cloud storage UI.
- All capabilities remain 100% self-hosted within the single Go binary with zero external build tools or runtime dependencies.
