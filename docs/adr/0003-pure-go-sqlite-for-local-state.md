# Pure-Go SQLite for Local State and Audit Trail

We will use `modernc.org/sqlite` (pure Go, CGO-free) for local metadata indexing, trash mapping, and rolling 100-event audit trail.

Cloudgate requires fast local full-text search, persistent mapping between soft-deleted files and their original remote paths, and atomic rolling audit logs. Using a pure Go SQLite driver ensures cross-compilation simplicity and zero C-compiler dependency, preserving the core requirement of shipping a single static binary.
