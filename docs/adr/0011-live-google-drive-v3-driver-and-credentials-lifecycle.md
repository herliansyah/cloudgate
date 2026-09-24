# Live Google Drive v3 Driver and Credential Lifecycle

We will implement a native, lightweight Google Drive v3 REST API driver with automatic token refreshing, persistent credential storage in SQLite, and full integration with the unified StoragePool.

## Context
Aggregating multiple cloud accounts into a single gateway requires live drivers that communicate directly with vendor APIs. For Google Drive:
1. Google enforces strict OAuth 2.0 web application policies: redirect URIs must not contain private LAN IP addresses (`192.168.x.x` or `10.x.x.x`), requiring normalization to `localhost:<port>`.
2. Newly created Google Cloud projects operate in "Testing" status, restricting access to developer-approved test users.
3. Newly created Google Cloud projects do not have the **Google Drive API** enabled by default, requiring explicit enablement in Google Cloud Console (`drive.googleapis.com`).
4. Tokens expire after 60 minutes, requiring automatic refresh using stored `refresh_token`, `client_id`, and `client_secret`.
5. Upon server restarts, connected drivers must be seamlessly rehydrated from persistent storage without requiring re-authentication.

## Decision
1. **Native REST Implementation**: Build `GDriveDriver` (`pkg/storage/gdrive.go`) using Go's standard `net/http` client without bloated external SDKs. Support `About` (quota), `List` (recursive and path queries), `Get` (streamed content), `Put` (multipart metadata + stream upload), `Delete`, `Move`, and `Mkdir`.
2. **Transparent Token Refreshing**: Implement thread-safe token freshness checks (`getValidAccessToken`), automatically exchanging `refresh_token` with Google's OAuth2 token endpoint before issuing Drive API calls.
3. **Database-Backed Credential Persistence**: Store OAuth tokens and client credentials within the `credentials` column of the SQLite `accounts` table. On gateway startup, deserialize credentials and re-register live `GDriveDriver` instances directly into the `all_pool` StoragePool.
4. **Resilient Error Propagation**: Detect and surface actionable Google Cloud API errors (such as `PERMISSION_DENIED` due to disabled Google Drive API or `redirect_uri_mismatch`) directly in the Web UI with direct activation links rather than silently masking them.
5. **Clean Account Disconnection**: Provide a clean disconnect lifecycle that simultaneously purges memory driver registries, removes the driver from all active StoragePools, wipes SQLite FTS search indices, and records an atomic audit event.
