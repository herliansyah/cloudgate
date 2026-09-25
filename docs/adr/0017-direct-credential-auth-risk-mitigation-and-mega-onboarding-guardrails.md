# 0017. DirectCredentialAuth Risk Mitigation and MEGA Onboarding Guardrails

Date: 2026-09-25

## Status

Accepted

## Context

Unlike standard cloud storage providers (such as Google Drive, Microsoft OneDrive, Dropbox, and Box) that provide official OAuth 2.0 delegated authorization flows with granular scopes and refresh tokens, MEGA (`mega.nz`) does not provide an official public OAuth 2.0 service. Integrating MEGA requires direct submission of master credentials (email and plaintext password) to derive client-side encryption keys.

When third-party applications (like Cloudgate via pure-Go API clients) authenticate against MEGA API endpoints:
1. MEGA's automated fraud heuristics and credential-stuffing prevention mechanisms track incoming API client fingerprints, IP addresses, and session frequencies.
2. Logins originating from non-residential IP addresses (such as cloud datacenter / VPS providers like DigitalOcean, AWS, GCP, Hetzner, etc.) or unknown client profiles frequently trigger MEGA's security alarm: *"Your MEGA account has been temporarily locked to protect your data after we detected a login by an unauthorised third party."*
3. Without explicit upfront risk disclosure, users linking their primary personal MEGA accounts from servers or repeated connection tests risk temporary account lockouts, requiring manual browser-based password resets and recovery keys.

## Decision

We introduce the canonical domain concept **`DirectCredentialAuth`** to model non-OAuth raw credential authentications, and implement strict operational guardrails and user disclosures across Cloudgate:

1. **Ubiquitous Language (`CONTEXT.md`)**:
   - Formally defined `DirectCredentialAuth` as a distinct, high-friction authentication flow contrasting with delegated `OAuthAuthorization`, documenting inherent risks of provider-enforced lockout and security challenges.

2. **Prominent UI Security Advisory Callout**:
   - In `web/dist/index.html` (under `megaExtraFields`), added a permanent high-visibility security alert banner explaining:
     - The lack of official OAuth in MEGA and the inherent risk of account lockout.
     - **Strict VPS Prohibition**: Explaining that running Cloudgate on VPS / datacenter IPs 99% triggers MEGA account lockout.
     - **Recommendation for Secondary Accounts**: Explicitly advising users to avoid primary accounts holding critical personal data.
     - **Mandatory 1x Browser Validation**: Prompting users to log in at `mega.nz` on the same network to legitimize their client IP.

3. **Mandatory Explicit Risk Acknowledgment Checkbox**:
   - Added `#megaConsentCheckbox` inside the onboarding modal.
   - Enforced client-side gatekeeping in `testAndOnboardAccount()`: verification is blocked with a clear toast notification until the user explicitly acknowledges the security risk.

4. **Context-Aware Error Diagnostics & Remediation**:
   - Enhanced `diagRemediationMsg` when MEGA tests fail to provide actionable steps: checking whether the account was locked by MEGA, direct link to `mega.nz` for unlocking and password reset, and IP/VPS verification.
   - Enhanced backend error formatting in `pkg/storage/rclone_adapter.go` to provide explicit failure context if `m.Login()` is rejected.

5. **Updated Help Center Documentation**:
   - Enriched the in-app documentation chapter for MEGA in `renderDocs()` with dedicated security FAQ covering lockout causes and recovery procedures.

## Consequences

- Prevents user frustration and unexpected account lockouts by mandating informed consent prior to submitting MEGA credentials.
- Establishes a clear boundary between safe delegated OAuth integrations and risky raw credential integrations.
- Maintains zero additional external dependencies and full compatibility with existing single-binary build processes.
