#!/usr/bin/env bash
# Release gating helper script for Cloudgate
# Enforces: clean git state, version parity in config, and updated CHANGELOG.md before tagging.

set -euo pipefail

# ponytail: simple bash gating script without external dependencies or heavy release frameworks

if [ $# -ne 1 ]; then
    echo "Usage: $0 <version> (e.g. $0 0.1.0 or $0 v0.1.0)" >&2
    exit 1
fi

RAW_VER="$1"
VERSION="${RAW_VER#v}"
TAG="v${VERSION}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "==> Verifying release conditions for ${TAG}..."

# 1. Check working directory is clean
if [ -n "$(git status --porcelain)" ]; then
    echo "ERROR: Working tree is dirty. Commit or stash all changes before releasing." >&2
    git status -s
    exit 1
fi

# 2. Check pkg/config/config.go version parity
CONFIG_FILE="pkg/config/config.go"
if [ ! -f "$CONFIG_FILE" ]; then
    echo "ERROR: $CONFIG_FILE not found." >&2
    exit 1
fi

CONFIG_VER=$(grep -E 'AppVersion\s*=' "$CONFIG_FILE" | sed -E 's/.*"([^"]+)".*/\1/')
if [ "$CONFIG_VER" != "$VERSION" ]; then
    echo "ERROR: Version mismatch!" >&2
    echo "  $CONFIG_FILE defines: '${CONFIG_VER}'" >&2
    echo "  Requested release is:   '${VERSION}'" >&2
    echo "Update AppVersion in $CONFIG_FILE first." >&2
    exit 1
fi

# 3. Check CHANGELOG.md has entry for this version
CHANGELOG_FILE="CHANGELOG.md"
if [ ! -f "$CHANGELOG_FILE" ]; then
    echo "ERROR: $CHANGELOG_FILE not found." >&2
    exit 1
fi

if ! grep -qE "^## \[${VERSION}\]" "$CHANGELOG_FILE"; then
    echo "ERROR: $CHANGELOG_FILE has no release section for '## [${VERSION}] - YYYY-MM-DD'." >&2
    echo "Promote changes from [Unreleased] into a dedicated version header first." >&2
    exit 1
fi

# 4. Check if git tag already exists
if git rev-parse "$TAG" >/dev/null 2>&1; then
    echo "ERROR: Git tag '${TAG}' already exists." >&2
    exit 1
fi

# 5. Create annotated git tag
echo "==> All pre-release checks passed!"
git tag -a "$TAG" -m "Release ${TAG}"
echo "SUCCESS: Created annotated git tag '${TAG}'."
echo ""
echo "Next step: Push branch and tag to remote when ready:"
CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
echo "  git push origin ${CURRENT_BRANCH} --tags"
