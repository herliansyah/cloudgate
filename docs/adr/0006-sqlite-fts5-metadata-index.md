# SQLite FTS5 Local Metadata Index for Unified Search

We will use an SQLite FTS5 table to index file names, paths, sizes, and timestamps across all connected RemoteAccounts, refreshed via incremental background synchronization.

Performing real-time searches across 10+ cloud storage provider APIs introduces multi-second latency, consumes severe API quota, and risks triggering provider rate-limiting bans. Local FTS5 indexing provides sub-15ms search latency and multi-criteria filtering without outbound network overhead during interactive queries.
