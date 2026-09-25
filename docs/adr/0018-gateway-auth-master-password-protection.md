# 0018. GatewayAuth and MasterPassword Protection

Date: 2026-09-25

## Status

Accepted

## Context

Cloudgate binds to `0.0.0.0` by default to enable convenient access across local network devices (LAN, home servers, NAS, mobile browsers). However, the Web UI and underlying REST API previously lacked access control. Anyone with network reachability to the listening port had unrestricted access to browse files across all cloud drives (`UnifiedExplorer`), configure cloud accounts (`StorageHub`), read cached credentials, initiate file transfers, or wipe data.

Users require an access control mechanism to protect their personal storage aggregator against unauthorized access on shared LANs without introducing the operational overhead of multi-tenant accounts, identity providers, or external dependencies.

## Decision

We introduce the canonical domain concepts **`GatewayAuth`** and **`MasterPassword`** into Cloudgate's architecture:

1. **Domain Model Alignment (`CONTEXT.md`)**:
   - `GatewayAuth`: The local access control system restricting access to Cloudgate's Web UI and REST API.
   - `MasterPassword`: The primary user-configured secret, cryptographically hashed and stored in the local SQLite database.

2. **Opt-in Activation**:
   - By default, Cloudgate remains open for maximum zero-friction desktop use.
   - Users can enable, configure, or change their `MasterPassword` via the StorageHub / Settings security section.

3. **Cryptographic Storage & Session Handling**:
   - The password hash is stored in SQLite using `bcrypt` (via `golang.org/x/crypto/bcrypt`, already present in the dependency tree).
   - Validated sessions issue a secure, HTTP-only cookie (`cg_session`) with a 7-day expiration window.

4. **Middleware & Endpoint Whitelisting**:
   - When `GatewayAuth` is enabled and no valid session cookie is provided:
     - Public access is permitted exclusively for static UI assets (HTML, CSS, JS, icons), gateway authentication endpoints (`/api/auth/gateway/*`), and third-party OAuth redirect callbacks (`/api/auth/{provider}/callback`).
     - All other REST API endpoints (`/api/accounts/*`, `/api/files/*`, `/api/pools/*`, `/api/trash/*`, `/api/stats/*`) reject unauthenticated requests with `401 Unauthorized`.

5. **Recovery Mechanism via CLI**:
   - If a user forgets their `MasterPassword`, they can execute `cloudgate auth reset` in their terminal. Physical/local CLI access proves machine ownership and safely clears the password hash without damaging connected storage accounts or file indices.

6. **Material 3 Interface Integration**:
   - When locked, the Single-Page Application displays a Material 3 Lock Screen dialog prompting for the `MasterPassword`.
   - The Top App Bar provides a one-click "Lock Gateway" button.
   - StorageHub provides dedicated settings for password creation, modification, and deactivation.

## Consequences

- Secures sensitive cloud credentials and personal files against unauthorized access across local Wi-Fi and homelab networks.
- Retains Cloudgate's single-binary, pure-Go, zero-external-dependency philosophy.
- Maintains zero friction for existing users until they explicitly choose to enable protection.
