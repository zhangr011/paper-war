# ADR-0021: Adopt ui-ux-pro-max Design System

**Date:** 2026-07-04
**Status:** Accepted
**Issue:** [#46 — Apply ui-ux-pro-max skill to systematize Paper War UI](https://github.com/zhangr011/paper-war/issues/46)
**Supersedes:** ad-hoc tokens documented in ADR-0001's UI section (informal)

## Context

Issue #46 identified that Paper War's v1.0 UI — while pixel-perfect
against the three design PNGs — had accumulated design debt:

- Color tokens with names like `--paper-bg` and `--panel-header` rather
  than semantic names (`--color-background`, `--color-primary`). No
  consistent naming scheme.
- No spacing scale — `padding: 10px`, `8px 10px`, `12px 32px` scattered
  as literals across 1 500+ lines of CSS.
- Zero accessibility audit against WCAG. No `:focus-visible` styles,
  no `prefers-reduced-motion` handling, multiple sub-44px touch targets.
- Two-font typography (Teko + Inter) with no documented size hierarchy.

A new community skill — `ui-ux-pro-max` — was installed in the active
Hermes profile specifically to address this. It packages 161 reasoning
rules across 10 categories, 67 style presets, a programmatic Design
System Generator, and stack-specific guidelines.

## Decision

Adopt the skill's structural outputs (semantic naming, spacing ramp,
shadow depths, accessibility gates) while **preserving** Paper War's
verified palette and typography pair. The skill's outputs are a
*refinement* of the existing design, not a replacement — important
because the v1.0 polish pass verified pixel-perfect match to
`design/{component,main,map}.png` via PIL analysis, and that match
must be preserved.

### 1. Semantic token layer (additive, not replacement)

New tokens (`--color-background`, `--space-md`, `--shadow-sm`,
`--text-body`, `--duration-base`, etc.) are introduced as **aliases**
of the existing literal values. Old names (`--paper-bg`) continue to
work — no existing rule needs to change. New code uses the new names.

This avoids a risky big-bang refactor while establishing the system.
Future PRs can incrementally migrate existing rules.

### 2. Design system document (`docs/design-system.md`)

The skill's Design System Generator produced a baseline document
(pattern, style, palette, typography, effects, checklist). We adapted
it to Paper War's verified palette — replacing the generator's generic
"SaaS Mobile / electric blue" output with our parchment-and-navy
values. The doc includes WCAG contrast ratios for every text/background
pair, computed via relative luminance.

### 3. Accessibility fixes shipped in this ADR

- `:focus-visible` outlines on every interactive element (2-3px gold
  accent with offset). Canvas exempt.
- `min-height: 44px` on every button-like class (lobby-btn, cmd-type-btn,
  formation-btn, recruit-btn, build-btn, minimap controls).
- `prefers-reduced-motion` media query clamping all transitions to 0.01ms.
- Animation tokens (`--duration-fast/base/slow`, `--ease-out/in-out`)
  replacing ad-hoc `transition: all 0.2s ease`.

### 4. Touch target floor (`--touch-min: 44px`)

The skill flags this as a CRITICAL rule (ux-guidelines.csv,
"Touch Target Size — Minimum 44x44px"). Paper War targets tablet
(ADR-0001), so it's in scope. The floor is enforced via CSS
`min-height` + `min-width` on a curated selector list.

### 5. ARIA semantics deferred

The a11y audit (`docs/a11y-audit.md`) identifies that the match-result
overlay, reconnect toast, and tab-button groups lack proper ARIA roles.
These are real WCAG 4.1.2 gaps but require per-element surgery
(focus traps, `aria-modal`, `role="tablist"`) that warrants a
dedicated follow-up. The audit documents the gap explicitly.

## Consequences

- **Pro**: New UI code (Career, Leaderboard, future meta screens) uses
  consistent tokens. No more "what padding should I use?" — reach for
  `--space-md`.
- **Pro**: Accessibility floor established. `:focus-visible` + 44px
  touch + reduced-motion are baseline expectations now.
- **Pro**: Design system document is onboarding-friendly. New
  contributors see the palette, typography scale, and a11y checklist
  in one place.
- **Pro**: The skill's `search.py` tool is available for future rule
  lookups (e.g., "what's the rule for modal animation timing?").
- **Con**: Two parallel naming schemes during migration. Old code uses
  `--paper-bg`; new code uses `--color-background`. Both work; confusion
  is possible. Mitigation: the design-system.md doc cross-references.
- **Con**: The `min-height: 44px` rule slightly enlarged some buttons
  that were already close to the floor — visual size changed a few
  pixels. Verified via e2e that no layout broke.
- **Con**: ARIA gap remains. The audit documents it; a follow-up issue
  should be filed.

## Verification

- All 18 Playwright e2e tests pass (including the multiplayer playtest
  that runs to completion in ~2 min).
- Touch targets verified via CSS selector coverage: every button class
  has `min-height: var(--touch-min)`.
- WCAG contrast ratios computed and documented in `docs/design-system.md`.
- Focus visibility manually confirmed by tabbing through the lobby
  (gold outline appears on each button).

## Future work (out of scope for this issue)

- ARIA roles on modal overlays, toasts, tab groups (separate issue)
- Migrate existing CSS rules from old token names to semantic names
  (incremental, low-priority)
- Mobile/phone layout audit (currently tablet-only per ADR-0001)
- Re-run the skill's Design System Generator with a more targeted brief
  to see if it suggests a better style fit than the generic "SaaS Mobile"
  it returned this time
