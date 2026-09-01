# Client UI — conventions & inventory

The always-loaded context for any AI (or human) touching the DOM/HUD layer.
Keep this file **artifact-derived**: when `style.css`, `index.html`, or the
module layout change, update the corresponding line here. It exists so a
generation pass starts from the real inventory instead of rediscovering it —
models without design context hard-code colors and write one-off CSS
(the monday.com failure mode; see `docs/research/ai-ui-workflow-2026.md`).

## Hard rules

1. **Tokens only.** No literal colors, radii, shadows, or ad-hoc spacing in
   new CSS. Prefer the semantic aliases (`--color-*`, `--space-*`) in
   `style.css:54+`; never re-derive a value the token layer already owns.
   Full system + WCAG verification: `docs/design-system.md` (ADR-0021).
2. **Art stays procedural.** Unit sprites are drawn by `src/unit_atlas.js`;
   DOM textures (parchment/wood/navy/gold/ink) by `scripts/gen-textures.py`
   (deterministic, byte-identical on re-run). Improve the drawing code;
   don't import PNGs. Override path for real art: `assets/sprites/`.
3. **No framework.** Vanilla JS ES modules + direct DOM. Patterns to match:
   pooled descriptor objects in render paths, id-based element lookup,
   screen switching via the `.screen`/`.active` classes (see below).
4. **Canvas boundary.** The WebGL canvas (`#game-canvas`) is opaque to QA
   tooling — headless capture can't read its pixels. DOM/HUD changes verify
   via screenshots + console errors; canvas/shader changes verify by code
   review + eyeballing a real display.

## Layout & screens

`index.html` stacks full-screen `<div class="screen">` sections; exactly one
carries `.active`. Order: `#login-screen`, `#lobby-screen` (war room: roster,
commander pick, queue buttons), `#career-screen`, `#leaderboard-screen`,
`#clash-screen` (team sizes, commander matrix, terrain picker), `#game-screen`.

## In-game HUD (`#game-screen`)

- `#top-bar` — player identity, resource cards (`#gold`, `#unit-count`,
  `#bases`, `#score`), `#timer`, `#mute-btn`, `#settings-btn`.
- `#game-viewport` — `#game-canvas` (WebGL) + `#base-alert-overlay`.
- `#bottom-bar` — three `.panel` sections: `#selection-panel` (commander /
  units / morale bar / status), `#minimap-panel` (`#minimap-canvas` + zoom
  buttons), `#action-panel` (contextual `.action-btn` grid).

## Module map (src/)

- `main.js` — game shell: WebGL render loop, camera, input, HUD binding
  (~39 direct DOM lookups), `ATTACK_DURATION_MS`.
- `app.js` — screens/lobby/network wiring (~32 DOM lookups), clash options
  (`clashTeam*Size`, `clashTerrain`).
- `state.js` — snapshot interpolation/jitter buffer, unit lifecycle
  (`attackTriggeredAt`, `dyingAt`), facing. Has its own test file.
- `unit_atlas.js` — procedural sprite atlas (7 unit types × 5 states ×
  4 directions).
- `particles.js`, `audio*`, `match_result.js` — combat FX, sound, result
  formatting; each with a `*_test.mjs`.

## Testing

`for f in client/src/*_test.mjs; do node "$f"; done` — all must stay green
(node-assert style, no framework). Snapshot/lifecycle changes need a test
in the matching `*_test.mjs` (see `state_facing_test.mjs` for the pattern).

## Workflow (verified best practice, Aug 2026)

Design variants → `/design-shotgun`; review pass → `/design-review`;
visual QA loop → `/qa` or `/browse` (DOM/HUD only, see rule 4); landing
gate → `/review` + green suite. Full rationale:
`docs/research/ai-ui-workflow-2026.md`.
