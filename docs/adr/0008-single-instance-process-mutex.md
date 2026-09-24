# Single-Instance Process Mutex via Lockfile

We will enforce a single active Cloudgate instance per machine using an exclusive filesystem lock (`cloudgate.lock`) that records the running process PID and active HTTP port.

Running multiple instances concurrently causes database write contention on SQLite, port binding conflicts, and race conditions during file transfers. When a user runs Cloudgate a second time, the new process detects the active lock, reads the listening port, automatically opens the running web interface in the browser, and cleanly exits.
