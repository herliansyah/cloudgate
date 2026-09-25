# 0021. Mandatory First-Run MasterPassword Setup with Loopback Guard

Date: 2026-09-25

## Status

Accepted (Partially supersedes [ADR 0018](0018-gateway-auth-master-password-protection.md))

## Context

In ADR 0018, `GatewayAuth` was introduced as an opt-in security mechanism while Cloudgate bound to `0.0.0.0` by default. Under that model, when `MasterPassword` had not yet been configured on a fresh installation, Cloudgate allowed unrestricted access to any incoming connection regardless of client IP address.

This created an unintended exposure window: if Cloudgate was run on a machine connected to a shared local network (LAN / Wi-Fi), any device on the network could access the Web UI and underlying REST API without entering credentials. Furthermore, relying on an opt-in workflow left instances unprotected unless users manually navigated to security settings.

Users required an access control model where all network access is protected by default, without introducing complex IP whitelist rules, and where initial password provisioning cannot be hijacked by an unauthenticated network peer.

## Decision

We update `GatewayAuth` and `MasterPassword` to enforce a mandatory, uniform first-run security invariant:

1. **Mandatory First-Run Requirement (No Open Mode)**:
   - Cloudgate no longer operates in an unauthenticated "open" state.
   - When no `MasterPassword` exists in SQLite (`!HasMasterPassword()`), Cloudgate enters a strict `SETUP_REQUIRED` state.
   - All data endpoints (`/api/accounts/*`, `/api/files/*`, `/api/pools/*`, `/api/trash/*`, `/api/stats/*`) reject requests with `401 Unauthorized` until `MasterPassword` is initialized.

2. **Loopback-Only First-Run Provisioning Guard**:
   - The initial `MasterPassword` creation endpoint (`POST /api/auth/gateway/setup`) is permitted **exclusively** from loopback connections (`127.0.0.1` and `::1`).
   - If a request to `/api/auth/gateway/setup` originates from a remote/non-loopback IP, the server rejects it with `403 Forbidden` (`Inisialisasi MasterPassword hanya diizinkan dari localhost`).
   - Remote clients attempting to load the Web UI while `SETUP_REQUIRED` are presented with a locked security screen advising them to initialize from localhost or run the CLI setup command.

3. **CLI First-Run Setup Support**:
   - To support headless servers, NAS units, and automated containers where no local web browser is available, a new CLI subcommand `cloudgate auth setup <password>` is provided to initialize `MasterPassword` directly in the terminal.

4. **Uniform Password Enforcement After Initialization**:
   - Once `MasterPassword` is configured, Cloudgate enforces `GatewayAuth` uniformly across all clients (both loopback and remote IP).
   - Validated sessions issue the standard 7-day `cg_session` cookie.

5. **Permanent Protection (Non-Disableable via Web UI)**:
   - The ability to disable `GatewayAuth` via the Web UI or REST API (`POST /api/auth/gateway/disable`) is eliminated.
   - Once initialized, Cloudgate cannot be downgraded to an unauthenticated mode through the browser.
   - Users may change their `MasterPassword` at any time via StorageHub.
   - Full password reset or recovery strictly requires local machine access via `cloudgate auth reset` in the terminal.

## Consequences

- Prevents unauthorized remote access on shared networks from the very first moment of installation.
- Eliminates the security risk of third parties on the LAN hijacking first-run password setup.
- Closes the vulnerability where authenticated web sessions or CSRF could disable security protection.
- Retains effortless multi-device access across LAN (phone, tablet, laptop) once the primary secret is initialized.
- Supersedes point 2 ("Opt-in Activation") of ADR 0018.
