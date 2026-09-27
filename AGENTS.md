## Agent skills

### Issue tracker

Local markdown issues tracked in `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical 5-role triage vocabulary. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout (`CONTEXT.md` and `docs/adr/`). See `docs/agents/domain.md`.

### Changelog & Release Lifecycle

- `CHANGELOG.md` adheres to [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/) and SemVer 2.0.0.
- All non-release commits add items under `## [Unreleased]`.
- Before cutting any release tag, update `pkg/config/config.go` (`AppVersion`), promote changes in `CHANGELOG.md` to `## [<version>] - YYYY-MM-DD`, commit, and run `./scripts/release.sh <version>`. Never create release git tags manually without passing the release script gating.
