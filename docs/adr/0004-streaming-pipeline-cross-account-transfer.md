# In-Memory Streaming Pipeline for Cross-Account Transfers

We will execute cross-account file copy and move operations using in-memory `io.Pipe` streaming with small chunk buffers (8MB-32MB) rather than staging temporary files to local disk.

Writing multi-gigabyte files to local disk during cross-account operations causes severe disk I/O bottlenecks and risks filling the host machine's drive. Streaming transfers keep the host footprint negligible. For move operations, source deletion is triggered strictly after successful completion and byte/checksum verification on the target.
