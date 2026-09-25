# 0019. UI Internationalization and Bilingual Documentation Strategy

Date: 2026-09-25

## Status

Accepted

## Context

Cloudgate is designed to be accessible to a global audience while providing first-class support for Indonesian users. Previously, the user interface in [`web/dist/index.html`](../../web/dist/index.html) had a mixture of English and Indonesian strings (e.g. Indonesian navigation headers alongside English action menus and tooltips), and documentation was exclusively maintained in English.

Furthermore, Cloudgate adheres strictly to a single-binary zero-external-dependency philosophy (*Ponytail principle*). Adding heavy third-party internationalization frameworks (such as npm `i18next` bundles or external runtime translation loaders) would introduce unwanted build complexity, runtime overhead, and bundle bloat.

Additionally, Cloudgate's architecture is grounded in Domain-Driven Design with a strict Ubiquitous Language defined in [`CONTEXT.md`](../../CONTEXT.md) (such as `StoragePool`, `RemoteAccount`, `StorageHub`, `UnifiedExplorer`, and `ConnectionPipeline`). Translating these canonical domain terms into literal local equivalents (e.g., "Lumbung Penyimpanan" or "Akun Jarak Jauh") would cause cognitive friction, desynchronize UI terminology from backend Go code models, and obscure core domain boundaries.

## Decision

We establish a lightweight, zero-dependency bilingual architecture (Bahasa Indonesia & English) across both the Web UI and public documentation:

1. **Zero-Dependency Inline i18n Engine**:
   - The Web UI implements an inline JavaScript translation dictionary (`I18N = { en: {...}, id: {...} }`) embedded directly within [`web/dist/index.html`](../../web/dist/index.html).
   - Static DOM elements declare `data-i18n="<key>"`, `data-i18n-title="<key>"`, or `data-i18n-placeholder="<key>"`.
   - A lightweight `applyTranslations()` routine traverses tagged elements and updates text and tooltips dynamically without page reloads.
   - Dynamic UI generators (modals, toasts, action bars) consume translation values via helper `t(key)`.

2. **Language Switching & Persistence**:
   - A dedicated Language Switcher toggle (`#btnLang`) is added to the Top App Bar (`.nav-actions`) adjacent to the Theme Switcher.
   - User language preference is persisted in browser storage via `localStorage.getItem('cloudgate-lang')`.
   - On first launch with no persisted preference, the application detects the user's browser language via `navigator.language` (defaulting to Indonesian if prefixed with `id`, otherwise defaulting to English).

3. **Preservation of Canonical Domain Terms (Ubiquitous Language)**:
   - All core domain terms (`StoragePool`, `RemoteAccount`, `VirtualFile`, `StorageHub`, `UnifiedExplorer`, `ConnectionPipeline`, `TrashRecord`, `EncryptedVault`, `GatewayAuth`, `MasterPassword`) remain preserved as canonical proper nouns across both English and Indonesian modes.
   - Auxiliary verbs, instructions, tooltips, dialogs, and descriptions are translated into clear, idiomatic Bahasa Indonesia.

4. **Bilingual Documentation Structure (`.id.md`)**:
   - Primary project files remain in English ([`README.md`](../../README.md) and [`CONTEXT.md`](../../CONTEXT.md)) to support standard developer tooling, package indexers, and automated coding agents.
   - Dedicated Indonesian counterparts are introduced as [`README.id.md`](../../README.id.md) and [`CONTEXT.id.md`](../../CONTEXT.id.md).
   - Each document features prominent top-level navigation cross-links (`[English](...) | [Bahasa Indonesia](...)`).

## Consequences

- Delivers a seamless bilingual experience (Bahasa Indonesia & English) with instant client-side switching.
- Preserves single-binary deployment with zero external npm or runtime HTTP dependencies.
- Maintains strict alignment between backend Go symbols, domain documentation, and front-end labels.
- Establishes a clean, scalable pattern for adding additional languages in the future if required.
