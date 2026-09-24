# 0014. Material Design 3 (M3) UI Redesign

Date: 2026-09-24

## Status

Accepted (Supersedes [0013-vanilla-glassmorphism-ui-redesign.md](0013-vanilla-glassmorphism-ui-redesign.md))

## Context

Cloudgate previously used a glassmorphism theme (blurs, translucent surfaces, and gradients). While visually modern, the user requested an evolution to Google's **Material Design 3 (M3)** (https://m3.material.io/) design system for improved usability, accessibility, standard tonal elevation, and structured component patterns.

At the same time, Cloudgate must strictly adhere to the single-binary deployment model (`go build -o bin/cloudgate`) with zero external runtime or build toolchain dependencies (no Node.js/npm, no external bundlers, and 100% offline self-hosted operability).

## Decision

We transitioned Cloudgate's UI to an authentic, zero-dependency **Material Design 3 (M3)** design system implemented entirely in vanilla HTML/CSS/JS inside `web/dist/index.html`:

1. **Design Tokens & Tonal Color System**:
   - Seed Color: Cloudgate Tech Indigo (`#3859FF` / `#4F7CFF`).
   - Implemented full M3 color roles for both Light and Dark modes (`--md-sys-color-primary`, `--md-sys-color-surface`, `--md-sys-color-surface-container-*`, `--md-sys-color-outline`, etc.).
   - M3 standard shape corner scales (Extra-Small 4px, Small 8px, Medium 12px, Large 16px, Extra-Large 28px, Full 9999px).
   - Replaced glassmorphism `backdrop-filter` and opacity overlays with M3 tonal container elevations and state layers (hover 8%, focus 12%, pressed 12%).

2. **Shell & Navigation**:
   - **M3 Navigation Drawer**: Persistent on desktop (≥840px), modal drawer with scrim on mobile (<840px). Features an Extended FAB ("New Upload") at the top, pill-shaped active destination indicators, and a storage capacity card with an M3 linear progress bar.
   - **M3 Top App Bar**: Features an integrated M3 pill-shaped Search Bar and transitions dynamically into a Contextual Action Bar when items/files are selected.

3. **Domain Entity Visualization**:
   - **VirtualFile Representation**: M3 Outlined Cards (12px radius, outline-variant, tonal hover state) in Grid view, and M3 List rows (8px radius, 56px height) in List view.
   - **RemoteAccount Attribution**: Embedded M3 Assist Chips directly on each file card/row showing the backing `RemoteAccount` and provider transparently.
   - **Unified M3 Side Sheet**: Right-hand side sheet serving both as a File Inspector (metadata, remote path, checksum, direct actions) and as the viewer for `RollingAuditLog` (last 100 operations).

4. **Dialogs, Forms & Feedback**:
   - Converted all modals to M3 Basic Dialogs (28px corner radius, surface-container-high) with M3 Outlined Text Fields and standard M3 button hierarchy (Text, Outlined, Filled).
   - M3 Snackbar for non-intrusive toast notifications and M3 Linear Progress Bar for active transfers.
   - 100% offline-ready embedded inline SVGs styled to M3 proportions and token colors.

## Consequences

- Zero build toolchain added; `go build` continues to build the entire web gateway into a single standalone binary.
- Full offline capability preserved without relying on external fonts or CDNs.
- Improved accessibility with strict contrast ratios defined by M3 color roles.
- Smooth transition between standard browsing and contextual multi-selection.
