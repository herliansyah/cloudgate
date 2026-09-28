#!/usr/bin/env bash
# Generate Windows PE resource .syso files for Cloudgate containing application icon and metadata.
# ponytail: generate COFF .syso with go-winres so 'go build' links icon natively without build dependencies

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

ICON_SRC="docs/assets/app-icon.png"

if [ ! -f "$ICON_SRC" ]; then
    echo "ERROR: Icon file '$ICON_SRC' not found." >&2
    exit 1
fi

echo "==> Generating Windows PE resource .syso files from ${ICON_SRC}..."

go run github.com/tc-hib/go-winres@v0.3.3 simply \
    --icon "$ICON_SRC" \
    --arch amd64,arm64 \
    --out cmd/cloudgate/rsrc \
    --file-description "Cloudgate Unified Cloud Gateway" \
    --product-name "Cloudgate"

echo "SUCCESS: Generated cmd/cloudgate/rsrc_windows_amd64.syso and rsrc_windows_arm64.syso"
