# Paper War Accessibility Audit

**Date:** 2026-07-04
**Auditor:** ui-ux-pro-max skill + manual review
**Scope:** DOM UI panels (lobby, career, leaderboard, clash, login, HUD overlay).
The WebGL canvas (game rendering) is out of scope — it has its own
accessibility considerations (alt text for state, screen-reader narration
of game events) that warrant a separate audit.

## Methodology

Two sources combined:

1. **`ui-ux-pro-max` skill rules** — queried the skill's `data/ux-guidelines.csv`
   for accessibility-related rules. Results cited below by rule name and
   severity.
2. **WCAG 2.1 AA criteria** — the canonical checklist from the W3C.

## Findings + actions

### 1. Focus indicators — FAILED, now FIXED

**Rule:** "Focus States — Use visible focus rings on interactive elements"
(`ux-guidelines.csv`, Severity: High)
**WCAG:** 2.4.7 Focus Visible (Level AA)

**Before:** no `:focus-visible` styles anywhere in `client/style.css`.
Keyboard users had zero visual indication of which element was focused.

**After:** added a global `:focus-visible` rule (commit reference: this
issue's PR) applying a 2-3px gold-accent outline with 2px offset to every
interactive element. Canvas is exempt (continuous-interaction surface).

```css
:focus-visible {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}
button:focus-visible {
  outline: 3px solid var(--color-accent);
  outline-offset: 2px;
}
```

**Status:** ✓ Resolved in this issue.

### 2. Touch target size — FAILED, now FIXED

**Rule:** "Touch Target Size — Minimum 44x44px touch targets"
(`ux-guidelines.csv`, Severity: High)

**Before:** `.lobby-btn` had `padding: 10px` + `font-size: 18px` →
effective ~38×40px. Several other buttons (cmd-type, formation, recruit,
build, minimap controls) were similarly undersized.

**After:** added `min-height: var(--touch-min)` (44px) and
`min-width: var(--touch-min)` to every button-like class. Table cells
exempt.

**Status:** ✓ Resolved in this issue.

### 3. Color contrast — PASS

**Rule:** "Color Contrast — Minimum 4.5:1 ratio for normal text"
(`ux-guidelines.csv`, Severity: High)
**WCAG:** 1.4.3 Contrast (Minimum) (Level AA)

Verified all primary text/background pairs. Full table in
`docs/design-system.md` § "WCAG AA contrast verification". Summary:

| Pair | Ratio | Result |
|------|-------|--------|
| Foreground (#1E293A) on Background (#E6E2D8) | 12.8:1 | ✓ AAA |
| Muted (#6B5E4E) on Background (#E6E2D8) | 4.7:1 | ✓ AA |
| On-primary (#F0E6D2) on Primary (#2C394F) | 10.1:1 | ✓ AAA |
| Accent (#C8A832) on Action-bg | 2.4:1 | ✗ as text — but only used as border |

**Status:** ✓ Pass. Accent-as-text is flagged but not currently used that way.

### 4. Reduced motion — FAILED, now FIXED

**Rule:** "Reduced Motion — Respect prefers-reduced-motion"
(`ux-guidelines.csv`, Severity: Medium)
**WCAG:** 2.3.3 Animation from Interactions (Level AAA)

**Before:** no `prefers-reduced-motion` media query. All transitions and
the spinning lobby queue indicator ran unconditionally.

**After:** added a global media query that clamps all transitions and
animations to 0.01ms when the user prefers reduced motion.

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    transition-duration: 0.01ms !important;
    animation-duration: 0.01ms !important;
  }
}
```

**Status:** ✓ Resolved in this issue.

### 5. Semantic HTML / ARIA — PARTIAL, deferred

**WCAG:** 4.1.2 Name, Role, Value (Level A)

The login screen uses a proper `<form>` and `<input>` — good. The lobby
buttons are `<button>` elements with text labels — good. But:

- The match-result overlay is a `<div>` with `display: none` toggle —
  should be `role="dialog"` + `aria-modal="true"` + focus trap.
- The reconnect toast is created via JS without `role="status"` or
  `aria-live="polite"`.
- The game canvas has no `aria-label` describing its purpose.
- Tab-button groups (cmd-type, formation) lack `role="tablist"` /
  `role="tab"` semantics.

**Status:** ⚠ Partial — fixes deferred to a follow-up issue. This audit
documents the gap; implementing ARIA on the modal overlays is
approximately 2 hours of focused work and should not block this issue's
merge.

### 6. Keyboard navigation order — PASS (informal)

Tab order through the login screen and lobby is logical
(top-to-bottom, left-to-right). The new `:focus-visible` outlines make
this manually verifiable. No `tabindex="-1"` or `tabindex > 0` abuses
found.

### 7. Color-only information — PASS

The fog-of-war grid uses color (dimmed vs. full-bright) to distinguish
explored from visible tiles, but this is reinforced by the tile content
itself (units present = visible; empty = explored). Match result uses
"Wins!" / "Losses" text in addition to color-coded rows. No color-only
state indicators found.

## Summary

| # | Check | Status |
|---|-------|--------|
| 1 | Focus indicators | ✓ Fixed |
| 2 | Touch targets ≥ 44×44 | ✓ Fixed |
| 3 | Color contrast ≥ 4.5:1 | ✓ Pass |
| 4 | prefers-reduced-motion | ✓ Fixed |
| 5 | ARIA semantics | ⚠ Deferred |
| 6 | Keyboard nav order | ✓ Pass |
| 7 | Color-only info | ✓ Pass |

5 of 7 checks pass or are fixed in this issue. ARIA semantics (item 5)
is the remaining gap, documented for a follow-up.

## Re-audit cadence

Re-run this audit whenever:
- A new screen is added (currently 7: login, lobby, clash, career,
  leaderboard, game, match-result-overlay)
- A new interactive element type is introduced
- The palette changes (re-verify contrast ratios in
  `docs/design-system.md`)

The skill's `search.py --domain ux "accessibility"` query can be used
to refresh the rule citations.
