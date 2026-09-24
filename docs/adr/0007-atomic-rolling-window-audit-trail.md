# Atomic Rolling Window Audit Trail

We will maintain a strictly capped 100-event audit log in SQLite using an atomic database trigger or transaction that purges older rows upon insertion.

User requirements specify an exact 100-event history audit trail. Managing retention at the database transaction layer guarantees strict bounds on storage growth, prevents unbounded row accumulation, and simplifies the export of a consistent 100-event log.
