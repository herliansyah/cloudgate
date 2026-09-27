#!/usr/bin/env bash
# Self-check test for scripts/release.sh logic
set -euo pipefail

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

SCRIPT_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release.sh"
mkdir -p "$TMP_DIR/scripts"
cp "$SCRIPT_SRC" "$TMP_DIR/scripts/release.sh"
chmod +x "$TMP_DIR/scripts/release.sh"

cd "$TMP_DIR"
git init -q -b main
git config user.name "Test User"
git config user.email "test@example.com"

mkdir -p pkg/config
echo 'package config; const AppVersion = "1.0.0"' > pkg/config/config.go
echo -e "# Changelog\n\n## [Unreleased]\n" > CHANGELOG.md
git add .
git commit -qm "initial commit"

# Test 1: Mismatched version
if ./scripts/release.sh 1.0.1 >/dev/null 2>&1; then
    echo "FAIL: Expected mismatched version to fail" >&2
    exit 1
fi

# Test 2: Missing version in CHANGELOG.md
if ./scripts/release.sh 1.0.0 >/dev/null 2>&1; then
    echo "FAIL: Expected missing changelog header to fail" >&2
    exit 1
fi

# Test 3: Dirty tree failure
echo -e "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-09-27\n- Initial release" > CHANGELOG.md
if ./scripts/release.sh 1.0.0 >/dev/null 2>&1; then
    echo "FAIL: Expected dirty tree to fail" >&2
    exit 1
fi

# Test 4: Success path
git add CHANGELOG.md
git commit -qm "update changelog for 1.0.0"

./scripts/release.sh 1.0.0 >/dev/null 2>&1
if ! git rev-parse "v1.0.0" >/dev/null 2>&1; then
    echo "FAIL: Tag v1.0.0 was not created" >&2
    exit 1
fi

# Test 5: Re-running on existing tag fails
if ./scripts/release.sh 1.0.0 >/dev/null 2>&1; then
    echo "FAIL: Expected existing tag to fail" >&2
    exit 1
fi

echo "All release.sh gating assertions passed successfully!"
