# Isolated Remote Trash with Local Catalog

We will isolate deleted files into a hidden remote folder (`/.cloudgate_trash/`) on each RemoteAccount and catalog their provenance in local SQLite instead of relying on vendor-native trash APIs.

Cloud storage providers differ drastically in trash behavior: some lack trash APIs entirely (e.g. S3, standard WebDAV), while others enforce unconfigurable auto-purge policies (e.g. 30 days) or discard original folder path hierarchy upon restoration. Maintaining a dedicated remote folder combined with local metadata ensures deterministic restorations to original paths and uniform behavior across all providers.
