# 0020. Account Principal Identity and StorageHub Presentation

Date: 2026-09-25

## Status

Accepted

## Context

In Cloudgate's StorageHub, connected cloud storage accounts were previously rendered with visual ambiguity and leaky internal abstractions:
1. In the provider column of the fleet table, the UI rendered both the human-friendly display name (`Google Drive`) and a capitalized internal slug (`GOOGLE`), creating redundant visual clutter.
2. In the account column and card view, the UI fell back to internal random identifier strings (`gdrive_174...`) or generic protocol labels (`OAuth`) whenever account email addresses were unavailable.
3. For Google Drive OAuth, the callback flow attempted to query Google's userinfo endpoint (`https://www.googleapis.com/oauth2/v2/userinfo`) without having requested profile/email scopes during authorization, resulting in HTTP 401 unauthorized errors and forcing the backend to fall back to generic strings such as `GOOGLE Account`.
4. Multi-cloud storage protocols have heterogeneous identity models: OAuth services (Google Drive, OneDrive, Dropbox, Box) and DirectCredentialAuth (Mega) use authenticated user emails; WebDAV instances use `username@host`; while object storage (S3 / MinIO) uses `bucket (endpoint)`. Lacking a canonical domain concept, these disparate identity attributes were either conflated or completely discarded.

## Decision

We establish the following architectural and domain design:

1. **Ubiquitous Language: `AccountPrincipal`**:
   - Introduce `AccountPrincipal` in [`CONTEXT.md`](../../CONTEXT.md) and [`CONTEXT.id.md`](../../CONTEXT.id.md) as the canonical domain representation of an external user or resource identity associated with a `RemoteAccount`.
   - Protocol-specific mapping:
     - **OAuth & DirectCredentialAuth (Google, OneDrive, Dropbox, Box, Mega)**: User's authenticated email address (e.g. `user@example.com`).
     - **WebDAV**: `username@host`.
     - **S3 / Object Storage**: `bucket (endpoint)`.
   - Never expose raw database IDs (`gdrive_174...`) in user-facing identity fields.

2. **Least-Privilege Identity Extraction for Google Drive**:
   - Rather than demanding intrusive personal profile/email scopes on the OAuth consent screen, Google Drive identity is fetched via the official Google Drive v3 `About` endpoint (`GET https://www.googleapis.com/drive/v3/about?fields=user`), which returns `user.emailAddress` and `user.displayName` under the existing Drive authorization scope.

3. **StorageHub Fleet Presentation & Naming Hierarchy**:
   - **Provider Presentation**: Render a single clean brand row (SVG Icon + Provider Display Name, e.g. `Google Drive`), eliminating the uppercase slug subline (`GOOGLE`).
   - **Account Hierarchy**:
     - Primary line: User-configured display alias (e.g. `Akun Pribadi`). If unspecified during onboarding, defaults intelligently to `<ProviderName> (<AccountPrincipal>)`.
     - Secondary line: Canonical `AccountPrincipal` rendered in monospace styling.
   - **Card View**: Card header displays `<Alias>` as the title and `<ProviderName> • <AccountPrincipal>` as the subtitle.

4. **Auto-Reconciliation & Backfilling**:
   - On server startup and during manual/scheduled `SyncSession` execution, existing accounts missing an `AccountPrincipal` are reconciled from live provider metadata, persisting the retrieved identity into SQLite and upgrading generic fallback labels.

## Consequences

- Completely eliminates visual redundancy in StorageHub table and card views.
- Adheres strictly to the Principle of Least Privilege for Google OAuth without triggering extra consent warnings.
- Unambiguously distinguishes multiple accounts from the same provider (e.g., personal vs. workplace Google Drive).
- Preserves Cloudgate's zero-dependency single-binary architecture.
