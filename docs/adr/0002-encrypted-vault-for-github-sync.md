# Encrypted Vault for GitHub Configuration Sync

We will encrypt all account configurations and credentials into an AES-256-GCM encrypted vault protected by a user master passphrase before syncing to GitHub.

Syncing account configurations to GitHub risks exposing sensitive OAuth refresh tokens, client IDs, and secret keys to GitHub secret scanners or public leaks. Storing credentials in an encrypted vault guarantees zero-knowledge portability across devices, allowing private or public GitHub repositories to be used as configuration backup targets.
