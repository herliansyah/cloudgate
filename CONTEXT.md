# Cloudgate

[English](CONTEXT.md) | [Bahasa Indonesia](CONTEXT.id.md)

A lightweight unified cloud storage gateway and multi-account aggregator compiled into a single Go binary.

## Language

**RemoteAccount**:
A configured and authenticated connection to a specific cloud storage provider with unique user credentials and capacity tracking.
_Avoid_: Drive connection, cloud user, session

**Provider**:
A supported third-party cloud storage service (e.g., Google Drive, OneDrive, Dropbox, S3, Mega).
_Avoid_: Vendor, cloud host

**StoragePool**:
A virtual aggregate of selected RemoteAccounts where file writes are distributed using capacity-aware round-robin.
_Avoid_: Bucket, cluster, drive group

**VirtualFile**:
A logical file representation within a StoragePool that maps directly to an intact physical file on a specific RemoteAccount.
_Avoid_: Chunk, striping block, virtual link

**TrashRecord**:
A soft-deleted item recorded in the local catalog preserving the original RemoteAccount and remote path for exact restoration.
_Avoid_: Recycle bin entry, tombstone

**AuditEvent**:
An immutable record of a user or system action, retained in a rolling window of the last 100 operations.
_Avoid_: Log entry, history item

**EncryptedVault**:
A cryptographically protected bundle (AES-256-GCM) containing sensitive account credentials and configurations suitable for safe synchronization to GitHub.
_Avoid_: Plain config, backup file

**RemoteTrash**:
A dedicated, isolated directory (`/.cloudgate_trash/`) maintained on each RemoteAccount to hold soft-deleted files until restoration or purging.
_Avoid_: Provider recycle bin, system trash

**AllocationPolicy**:
The rule set governing file placement within a StoragePool, evaluating available quota and account connectivity before assignment.
_Avoid_: Load balancer, placement algorithm

**TransferSession**:
An in-memory streaming pipeline mediating cross-account file copy or move operations using piped buffers and post-transfer verification.
_Avoid_: Sync task, file job

**MetadataIndex**:
A local, SQLite-backed cache of file hierarchy and attributes supporting Full-Text Search across all accounts without issuing live provider API calls.
_Avoid_: File list cache, catalog dump

**RollingAuditLog**:
A strictly bounded 100-event log stored in SQLite that tracks system and user file manipulations with atomic eviction of entries beyond the 100th record.
_Avoid_: Audit table, system journal

**InstanceLock**:
A local file mutex recording PID and listening port that prevents concurrent execution of multiple Cloudgate processes on the same machine.
_Avoid_: PID file, process guard

**ReleasePackage**:
An official binary release asset hosted on GitHub Releases used for automated version checks and self-updates.
_Avoid_: Update bundle, binary patch

**OAuthAuthorization**:
The protocol handshake granting Cloudgate access tokens and refresh tokens from third-party identity providers without storing plaintext passwords.
_Avoid_: Login session, API handshake

**DirectCredentialAuth**:
A high-friction authentication flow where Cloudgate manages raw user credentials (such as username/email and master password) to negotiate sessions directly with providers lacking standard delegated OAuth (notably MEGA), carrying inherent operational risks of third-party fraud lockout, IP flagging, and security challenges.
_Avoid_: Password login, basic auth, direct connection

**VendorDriver**:
A thin adapter over an embedded `rclone/fs.Fs` that implements the uniform Driver interface (`About`, `List`, `Get`, `Put`, `Delete`, `Move`, `Mkdir`) by delegating chunked uploads, retry, rate-limiting and provider quirks to rclone while preserving single-binary deployment without an external executable.
_Avoid_: Raw REST client, client wrapper, connector plugin

**CredentialPersistence**:
The local SQLite-backed mechanism that securely stores OAuth refresh tokens and client secrets in the `accounts.credentials` column and, on restart, rehydrates live VendorDrivers by injecting those credentials into in-memory `rclone/fs` configs without ever writing a persistent `rclone.conf` to disk.
_Avoid_: Token cache, session store, rclone.conf file

**AccountDisconnection**:
The coordinated teardown process that detaches a RemoteAccount, evicts its active Driver from all StoragePools, wipes its cached MetadataIndex entries, and purges its stored credentials.
_Avoid_: Account removal, unmount

**RemoteAccountUpdate**:
The process of modifying non-sensitive configuration parameters (such as display alias, root mount folder, and allocated quota) of an existing RemoteAccount, triggering selective cache invalidation without severing active provider authentication.
_Avoid_: Account edit, profile update

**StorageHub**:
The central control and telemetry view dedicated to managing all connected RemoteAccounts, aggregating quota distributions, providing connection diagnostics, and triggering system-wide synchronization.
_Avoid_: Accounts page, settings screen, connection list

**UnifiedExplorer**:
The consolidated file browsing and manipulation interface presenting an aggregate virtual hierarchy across all enabled RemoteAccounts as a single seamless storage system.
_Avoid_: Drive view, file manager window, bucket explorer

**ConnectionPipeline**:
The structured 5-phase onboarding protocol for attaching a new RemoteAccount: Provider Selection → Authentication → Connection Diagnostic Testing → Storage Quota Retrieval → Account Persistence.
_Avoid_: Connect wizard, add account popup, login form

**AccountIntegrationState**:
The operational availability flag (`enabled` or `disabled`) of an authenticated RemoteAccount, allowing an account to be temporarily excluded from write distribution and sync pools without severing credentials or deleting cached metadata.
_Avoid_: Account pause, sleep mode, disable toggle

**SyncSession**:
A manual or scheduled reconciliation routine ("Sync Now") that queries live provider drivers to refresh quota statistics, verify remote reachability, and synchronize the local MetadataIndex.
_Avoid_: Re-scan, refresh task, cache reload

**StarredRecord**:
A user-flagged bookmark pointing to a specific VirtualFile or path stored in the local SQLite catalog for instant cross-account retrieval.
_Avoid_: Favorite item, pinned file, bookmark

**ShareLink**:
A temporary or direct access URL generated for a VirtualFile, utilizing native provider sharing capabilities or Cloudgate's authenticated streaming gateway.
_Avoid_: Public URL, download link

**GatewayAuth**:
The local access control system that restricts access to Cloudgate's web interface and REST API endpoints until a valid session is established.
_Avoid_: Login page, user account system, web guard

**MasterPassword**:
The primary user-configured secret, cryptographically hashed and stored in the local SQLite database, used to unlock GatewayAuth and gain full administrative control of Cloudgate.
_Avoid_: User password, login credential, account pin


