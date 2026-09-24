# Vanilla Glassmorphism Redesign — Keep Single-File SPA

## Context
User asked for "lebih modern dan kekinian" UI. Current `web/dist/index.html` is a single-file vanilla SPA embedded via `web/web.go:9` (`embed.FS`) — zero Node toolchain, single Go binary. Alternatives (Vite + Tailwind + shadcn/React) would add `npm` build, bundle step, and increase binary embedding complexity, contradicting the lightweight single-binary promise (`README.md` Quick Start: `go build -o bin/cloudgate`).

Grill R1 decisions (user approved "gas sesuai rekomen"):
- Scope: facelift kosmetik, not UX restructure — keep sidebar(282px)+topbar(64px)+toolbar+grid/list flow.
- Visual: glassmorphism + shadcn clean (blur, translucent surfaces, gradient primary `linear-gradient(135deg, #4f7cff→#7c5cff→#a855f7)`), rounded 14–22px, soft shadows.
- Tech: stay vanilla vanilla CSS variables + `backdrop-filter: blur(18px) saturate(1.25)` — no new dependency. CDN Tailwind considered but adds runtime cost; deferred.
- Theme: dark (default) + light toggle via `data-theme` + `localStorage:cloudgate-theme`, respects `prefers-color-scheme`.
- Responsive: desktop-first but mobile usable — sidebar off-canvas with hamburger + overlay at ≤860px, grid collapses to 2-col at ≤520px.

## Decision
Keep **single-file vanilla HTML/CSS/JS** in `web/dist/index.html`. Modernize purely via CSS tokens (`--surface`, `--border`, `--grad-primary`, etc.), `backdrop-filter` glass on header/sidebar/cards/modals, and minimal JS for theme + sidebar drawer. No build toolchain added. All existing API contracts and element IDs (`#accountsNavList`, `#contentArea`, `#searchInput`, etc.) preserved — skin-only change.

## Consequences
- `go build` stays toolchain-free; no `package.json`, no Node in CI.
- Light/dark is CSS-only swap — no extra assets.
- Responsive drawer adds hamburger JS (`toggleSidebar`) but no layout breaking on desktop.
- Future: Vite+Tailwind/shadcn can be adopted when design system outgrows vanilla (e.g., need component library, animations scale). Defer until measurable pain.
- Glass `backdrop-filter` requires modern browser; fallback is opaque surface on older browsers — acceptable.
