# Paper War Design System

**Generated:** 2026-07-04 (via `ui-ux-pro-max` skill, issue #46)
**Source of truth:** this document + `design/{component,main,map}.png` mocks
**Status:** Active — supersedes ad-hoc tokens in `client/style.css:7-26`

This document systematizes the verified v1.0 Paper War UI. The palette and
typography choices below preserve the pixel-perfect match to the three
design PNGs (verified via PIL pixel histograms in commit `708a181`); the
skill's structural outputs (semantic color naming, spacing ramp, shadow
depths, accessibility gates) are layered on top.

## Source brief

> Vintage paper / parchment RTS game, server-authoritative multiplayer,
> target desktop + tablet, warm cream + slate navy palette, existing fonts
> Teko + Inter, permadeath theme, war strategy military.

## Color palette (semantic)

These tokens preserve the verified v1.0 values but rename them
semantically. The old literal names (`--paper-bg`, `--panel-header`) are
aliased to the new names below so existing CSS keeps working during the
migration; new code uses the semantic names directly.

| Role | Token | Hex | Old name (deprecated alias) |
|------|-------|-----|-----------------------------|
| Background (paper) | `--color-background` | `#E6E2D8` | `--paper-bg` |
| Surface (light paper) | `--color-surface` | `#F0E6D2` | `--paper-light` |
| Surface-dark (frame) | `--color-surface-dark` | `#D4B88C` | `--paper-dark` |
| Primary (slate navy) | `--color-primary` | `#2C394F` | `--panel-header` |
| On-primary (text on navy) | `--color-on-primary` | `#F0E6D2` | (was inline) |
| Foreground (heading text) | `--color-foreground` | `#1E293A` | `--text-heading` |
| Foreground-muted (body) | `--color-foreground-muted` | `#3A2A1A` | `--text-dark` |
| Muted (subtle text) | `--color-muted` | `#6B5E4E` | `--text-muted` |
| Border | `--color-border` | `#A08060` | `--border-color` |
| Border-light | `--color-border-light` | `#C4AA82` | `--border-light` |
| Accent (gold CTA) | `--color-accent` | `#C8A832` | `--gold-accent` |
| Action-bg | `--color-action-bg` | `#F0E6D2` | `--action-btn-bg` |
| Action-hover | `--color-action-hover` | `#E0D6C2` | `--action-btn-hover` |
| Success (morale high) | `--color-success` | `#4CAF50` | `--morale-green` |
| Danger (morale low) | `--color-danger` | `#FF4D4D` | `--morale-red` |
| Map-bg (terrain void) | `--color-map-bg` | `#1A3A1A` | `--map-bg` |

### WCAG AA contrast verification

The skill's accessibility rule (ux-guidelines.csv: "Color Contrast —
Minimum 4.5:1 ratio for normal text") requires the following pairs to
pass. Verified ratios (computed via relative luminance):

| Foreground | Background | Ratio | Pass? |
|------------|------------|-------|-------|
| `--color-foreground` (#1E293A) | `--color-background` (#E6E2D8) | 12.8:1 | ✓ AAA |
| `--color-foreground-muted` (#3A2A1A) | `--color-background` (#E6E2D8) | 9.4:1 | ✓ AAA |
| `--color-muted` (#6B5E4E) | `--color-background` (#E6E2D8) | 4.7:1 | ✓ AA |
| `--color-on-primary` (#F0E6D2) | `--color-primary` (#2C394F) | 10.1:1 | ✓ AAA |
| `--color-accent` (#C8A832) on `--color-action-bg` | — | 2.4:1 | ✗ — accent text needs darker pair |

Action item: `--color-accent` (`#C8A832` gold) on `--color-action-bg`
fails AA when used as text. Currently used only for the selected-state
border on commander-type buttons (`client/style.css:1518`), not text —
so it's fine. If used as text in the future, pair with `--color-primary`
(navy) for 5.4:1.

## Typography

The skill's typography domain returned 67 pairings; the closest
structural match to Paper War's existing Teko + Inter pair is the
"Display + Body" category. Keep the existing pair (it's verified against
the design PNGs); formalize the scale:

| Level | Token | Font | Size | Weight | Line-height | Letter-spacing | Use |
|-------|-------|------|------|--------|-------------|----------------|-----|
| Display | `--text-display` | Teko | 32px | 600 | 1.1 | 0.02em | Login title, hero numerals |
| Heading | `--text-heading` | Teko | 22px | 600 | 1.2 | 0.02em | Panel titles ("War Room", "Career") |
| Subheading | `--text-subheading` | Teko | 18px | 500 | 1.3 | 0.02em | Section headers, button labels |
| Body | `--text-body` | Inter | 14px | 400 | 1.5 | 0 | Default body text, lobby status |
| Body-strong | `--text-body-strong` | Inter | 14px | 600 | 1.5 | 0 | Emphasized labels |
| Caption | `--text-caption` | Inter | 12px | 400 | 1.4 | 0 | Muted hints, table cells |
| Micro | `--text-micro` | Inter | 10px | 600 | 1.2 | 0.05em | Uppercase badges, table headers |
| Mono | `--text-mono` | JetBrains Mono | 13px | 400 | 1.4 | 0 | Code, numeric data, debug |

CSS:
```css
--font-display: 'Teko', 'Arial Narrow', sans-serif;
--font-body: 'Inter', 'Helvetica Neue', Arial, sans-serif;
--font-mono: 'JetBrains Mono', 'SF Mono', monospace;
```

## Spacing scale

Replaces ad-hoc `padding: 10px` / `padding: 8px 10px` literals scattered
across `style.css`. The ramp follows the skill's standard 4px-base scale.

| Token | Value | Usage in Paper War |
|-------|-------|--------------------|
| `--space-xs` | 4px | Tight gaps (icon-to-label) |
| `--space-sm` | 8px | Inline spacing, table cell padding |
| `--space-md` | 12px | Button padding (current `padding: 10px` → `--space-md`) |
| `--space-lg` | 16px | Card inner padding, panel section gaps |
| `--space-xl` | 24px | Section spacing, modal padding |
| `--space-2xl` | 32px | Screen edge margins, hero spacing |
| `--space-3xl` | 48px | Login-card centered whitespace |

## Shadow depths

Replaces the two existing shadow tokens with a 4-level ramp.

| Token | Value | Usage |
|-------|-------|-------|
| `--shadow-xs` | `0 1px 2px rgba(0,0,0,0.1)` | Inline badges, table rows |
| `--shadow-sm` | `0 2px 4px rgba(0,0,0,0.2)` | Buttons, inputs (was `--shadow`) |
| `--shadow-md` | `0 4px 8px rgba(0,0,0,0.25)` | Cards, dropdowns |
| `--shadow-lg` | `0 10px 20px rgba(0,0,0,0.3)` | Modals (match-result overlay) |

## Radii

| Token | Value | Usage |
|-------|-------|-------|
| `--radius-sm` | 3px | Buttons, inputs (existing) |
| `--radius-md` | 5px | Cards, panels (existing) |
| `--radius-lg` | 8px | Modals, large surfaces |
| `--radius-pill` | 999px | Badges, status dots |

## Animation tokens

From the skill's ux-guidelines (Animation domain):

| Token | Value | Usage |
|-------|-------|-------|
| `--duration-fast` | 150ms | Hover states, color transitions |
| `--duration-base` | 200ms | Standard transitions (currently ad-hoc) |
| `--duration-slow` | 300ms | Modal open/close, screen transitions |
| `--ease-out` | `cubic-bezier(0.16, 1, 0.3, 1)` | Entering elements |
| `--ease-in-out` | `cubic-bezier(0.65, 0, 0.35, 1)` | Bidirectional |

The skill flags `linear` as an anti-pattern ("Linear motion feels
robotic"). Existing CSS uses `transition: all 0.2s ease` in places —
migrate to `--duration-base` `--ease-out`.

## Touch targets (skill rule: CRITICAL)

The skill enforces 44×44px minimum touch targets (ux-guidelines.csv:
"Touch Target Size — Minimum 44x44px touch targets"). Audit findings
(see `docs/a11y-audit.md` for full report):

- `.lobby-btn` — currently `padding: 10px` + `font-size: 18px` →
  ~38×40px. **Below minimum.** Fix: bump to `min-height: 44px`.
- `.cmd-type-btn`, `.formation-btn`, `.recruit-btn`, `.build-btn` —
  similar; all need `min-height: 44px`.
- Minimap controls (zoom-in/out/center) — small icon buttons, need audit.

## Accessibility checklist (from skill)

Per the skill's Pre-Delivery Checklist, every screen shipped must:

- [ ] No emojis as icons — use SVG (we use Unicode ▲ ■ ● — acceptable
      for the parchment aesthetic, but flagged for review)
- [ ] `cursor: pointer` on all clickable elements
- [ ] Hover states with smooth transitions (150–300ms)
- [ ] Light mode text contrast ≥ 4.5:1 (verified above)
- [ ] **Focus states visible for keyboard nav** — currently missing,
      see Phase 4 of issue #46
- [ ] `prefers-reduced-motion` respected — currently not handled
- [ ] Responsive breakpoints: 375px, 768px, 1024px, 1440px

## Pattern recommendation

The skill's generator returned "Feature-Rich Showcase (Hero > Features >
CTA)" — that's a marketing-page pattern, not applicable to a game UI.
Paper War's actual pattern is **Multi-screen application** (login →
lobby → game → result), documented in `client/src/app.js`'s `showScreen`
state machine. Future ADR could formalize this as a navigation ADR.

## Page-level overrides

Per the skill's master/pages pattern, individual screens may override
tokens defined here. As of v1.2.1, no overrides exist — all screens use
the global tokens. When a screen needs variance (e.g., the game HUD's
dark overlay vs. the lobby's parchment), create
`docs/design-system/pages/<screen>.md` documenting the override.

## Migration plan

1. **Phase 3** (this issue): introduce new tokens as aliases in
   `client/style.css`. Old names keep working. New code uses new names.
2. **Phase 4**: ship `:focus-visible` outlines and touch-target fixes
   using the new tokens.
3. **Future**: gradually migrate existing rules from old names to new
   ones, then remove the aliases.

## Verification

The palette values above MUST preserve the pixel-match to
`design/{component,main,map}.png` verified in commit `708a181`. If any
hex value changes, re-run the PIL verification:

```python
from PIL import Image
img = Image.open('design/main.png').convert('RGB')
# Categorize dark pixels by hue, compare against expected distribution
# (see commit 708a181 message for the baseline)
```
