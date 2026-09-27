# 0025. Changelog Specification and Release Gating

Date: 2026-09-27

## Status

Accepted

## Context

Cloudgate distributes standalone binaries through GitHub Releases and provides in-app self-update checks (`pkg/updater/`). As the project matures and features are continuously developed across multiple cloud storage drivers, a transparent, human-readable record of user-facing changes is required.

Previously, version increments and releases lacked a formal gating mechanism:
1. Changes were only traceable through scattered git commit histories and pull requests.
2. Binary releases risked shipping without user-facing documentation detailing added capabilities, breaking alterations, or security fixes.
3. Git tags could be pushed arbitrarily without verifying whether the application metadata (`config.AppVersion`) and release notes were synchronized.

## Decision

We establish a formalized `ReleaseLifecycle` standard and pre-release gating protocol:

1. **Keep a Changelog 1.1.0 & SemVer 2.0.0**:
   - A single root-level `CHANGELOG.md` file maintained in English.
   - All in-flight development changes are staged under an ongoing `## [Unreleased]` section.
   - Categorization strictly utilizes standard headers: `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, and `Security`.
   - Versions adhere to Semantic Versioning (`MAJOR.MINOR.PATCH`).

2. **Mandatory Release Gating (`scripts/release.sh`)**:
   - Every official release tag requires updating and finalizing `CHANGELOG.md` beforehand.
   - The lightweight helper script `./scripts/release.sh <version>` enforces:
     a. Working directory must be clean (no unstaged/uncommitted files).
     b. `config.AppVersion` in `pkg/config/config.go` must match the target version string.
     c. `CHANGELOG.md` must contain a dedicated release header `## [<version>] - YYYY-MM-DD` with documented items moved out of `[Unreleased]`.
   - Upon successful verification, the script creates an annotated local git tag `v<version>`.

3. **Domain Vocabulary Integration**:
   - `ReleaseLifecycle` and `Changelog` are formalized as first-class domain terms in `CONTEXT.md` and `CONTEXT.id.md`.

## Consequences

- End users and integrators receive curated, structured change logs alongside every binary release.
- Tagging and distribution mistakes (e.g. untracked releases, mismatching version numbers) are caught locally before reaching GitHub Releases or triggering automated updaters.
- Overhead remains lightweight: a single markdown document and a zero-dependency POSIX shell check.
