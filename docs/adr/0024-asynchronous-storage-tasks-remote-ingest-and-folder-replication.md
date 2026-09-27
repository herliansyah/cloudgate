# 0024. Asynchronous Storage Tasks, Remote Ingest, and Folder Replication

Date: 2026-09-27

## Status

Accepted

## Context

Cloudgate previously handled file transfers (`CopyFile` and `MoveFile` in `pkg/storage/transfer.go`) synchronously within the HTTP request lifecycle. This presented several operational limitations:
1. **HTTP Request Timeouts**: Large file transfers (e.g. >100MB) or transfers between slow cloud remotes exceed typical HTTP client and reverse proxy timeouts (504 Gateway Timeout).
2. **Lack of Directory Support**: Transfers only supported single discrete files, with no mechanism for recursive directory copies or moves.
3. **No Process Resilience**: Any transient network disruption or server restart immediately aborts transfers without state recovery.
4. **No Direct Remote Fetch**: Users needing to ingest web assets into their cloud drives had to first download them locally and subsequently upload them to Cloudgate.
5. **No Directory Synchronization**: Users had no mechanism to synchronize or mirror folder contents between different cloud providers (e.g. Google Drive to OneDrive).

## Decision

We introduce an asynchronous background task processing subsystem in Cloudgate adhering to the single-binary, pure-Go SQLite architecture:

1. **Persistent Task Catalog (`storage_tasks`)**:
   - A dedicated SQLite table tracking background tasks across process restarts with fields: `id`, `type` (`transfer`, `replicate`, `ingest`), `source_account_id`, `source_path`, `target_account_id`, `target_path`, `status` (`pending`, `running`, `completed`, `failed`, `cancelled`), `progress_bytes`, `total_bytes`, `items_processed`, `total_items`, `error_message`, `created_at`, `updated_at`.
   - On server startup, any tasks lingering in `running` status are automatically transitioned to `interrupted`/`failed`, enabling manual user retry without risking thundering-herd API quotas.
   - Task history is bounded using a rolling window of the last 100 finished records to prevent SQLite database bloat.

2. **Fixed-Worker Execution Pool**:
   - A bounded pool of 2 concurrent worker goroutines pulls pending tasks in FIFO order.
   - Tasks support context cancellation via an in-memory cancel map keyed by task ID.

3. **Recursive Directory Transfer (`TransferFolder`)**:
   - Walks the source driver recursively, creating directories and streaming files sequentially into the target driver.
   - Employs a continue-on-error strategy: failures are aggregated in the task record while non-failing files continue transfer.

4. **Directory Replication Engine (`ReplicationSession`)**:
   - One-way folder synchronization from source to target remote accounts.
   - Reconciles files using path and size comparisons.
   - Default mode is Additive (new and modified files copied; destination-only files preserved).
   - Optional Mirror mode soft-deletes destination files that no longer exist on the source by routing them into Cloudgate's `RemoteTrash` (`/.cloudgate_trash/`).

5. **Direct Remote Ingest with SSRF Protection (`RemoteIngest`)**:
   - Zero-disk streaming pipeline fetching HTTP/HTTPS resources directly into the designated remote account via `io.Pipe`.
   - Enforces strict SSRF validation at the network boundary, blocking loopback addresses (`127.0.0.0/8`, `::1`), RFC1918 private subnets (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), and cloud link-local metadata endpoints (`169.254.169.254`).
   - Resolves target filenames using a 4-tier hierarchy: custom parameter $\to$ `Content-Disposition` header $\to$ URL path basename $\to$ timestamped fallback.

6. **Web Interface (`TaskDrawer`)**:
   - An interactive floating drawer docked in the lower-right viewport displaying active tasks with progress indicators, error badges, and cancel/retry controls.

## Consequences

- Long-running cloud transfers, folder syncs, and remote downloads execute reliably in the background without blocking client connections or browser tabs.
- Bounded concurrency prevents third-party API rate-limiting (HTTP 429) on Google Drive and OneDrive.
- SSRF guards safeguard local and cloud private network resources.
- Preserves single-binary zero-CGO deployment without requiring external message brokers (Redis, RabbitMQ) or external cron services.
