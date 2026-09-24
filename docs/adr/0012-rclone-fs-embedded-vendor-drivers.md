# Embed rclone/fs as VendorDriver Engine for All Providers

We will re-anchor Cloudgate on the original ADR-0001 decision: embed `rclone/fs` as a Go library inside the single static binary and implement every VendorDriver as a thin adapter over `rclone/fs.Fs`, superseding the one-off native REST approach of ADR-0011.

## Context
ADR-0001 prescribed `rclone/fs` for 10+ providers, but the live implementation diverged: only `GDriveDriver` (`pkg/storage/gdrive.go`) was built as native `net/http` REST, leaving `go.mod` with zero rclone dependency (`grep -r rclone` only hit docs). The `Driver` interface (`pkg/storage/driver.go:34`) stayed correct, but `pkg/server/server.go:446` hardcoded `NewGDriveDriver` for every OAuth callback, so OneDrive/Dropbox tokens were wrapped as GDrive and failed. Extending 9 more providers with hand-rolled REST would duplicate chunking, backoff, token refresh, and provider quirks already solved by rclone, violating the single-binary, zero-external-dependency promise if we shell-out.

## Decision
1. **Adapter pattern**: Add `pkg/storage/rclone_adapter.go` (and per-provider constructors like `NewOneDriveDriver`, `NewDropboxDriver`, `NewS3Driver`, etc.) that wrap an `fs.Fs` instance and implement `Driver` (`About`, `List`, `Get`, `Put`, `Delete`, `Move`, `Mkdir`) by mapping to `fs` operations. Keep `GDriveDriver` temporarily for reference/tests, but new providers and eventual GDrive migration use the adapter.
2. **In-memory credential injection**: Keep `CredentialPersistence` in SQLite `accounts.credentials` (`pkg/db/db.go`). On `ExchangeCode` (`pkg/auth/oauth.go:104`) and on boot (`cmd/cloudgate/main.go` rehydration), inject `client_id/refresh_token/access_token` into rclone's in-memory config (e.g., `fs.Config` / `oauthutil`) — never write a persistent `rclone.conf`. This preserves `EncryptedVault` + `sync/github.go` flows.
3. **Retain domain semantics**: `StoragePool`, `TransferSession` (`io.Pipe` in `pkg/storage/transfer.go`), `RemoteTrash` (`/.cloudgate_trash/`), and `MetadataIndex` FTS5 stay provider-agnostic; the adapter just supplies `About`/`List`/streaming `Get`/`Put`.
4. **Supersedes**: Re-affirms ADR-0001, amends ADR-0011 §1 (native REST) from a universal strategy to a single-provider legacy: future providers must use rclone, with GDrive optionally migrated later.

## Consequences
- `go.mod` gains `github.com/rclone/rclone` (and transitive deps); binary grows ~15-30 MB and build time increases, but 40+ providers become available without per-provider REST rewrites.
- `pkg/auth/oauth.go:39` `ProviderConfigs` remains the source of `AuthEndpoint`/`TokenEndpoint` for consent URL generation; rclone handles refresh thereafter.
- Private-IP redirect normalization (`pkg/server/server.go:282`) and Google API-enable guidance remain in server layer, not pushed into rclone.
