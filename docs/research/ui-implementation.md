# UI Implementation — Research Note

**Date:** 2026-08-09
**Trigger:** Vague user ask "ui implement for our project." This note grounds that ask in what already exists, then surfaces primary-source-backed guidance for whatever the user actually meant.

---

## TL;DR

paper-war **already has a substantial, working UI**: a vanilla-JS, framework-free client with a WebGL2 game canvas, a full DOM-overlay HUD (login, lobby, career, leaderboard, clash config, in-game), a procedural parchment/navy design system (ADR-0021), and three 2D-canvas dev editors. "Implement UI" is therefore ambiguous — it most likely means **extend/polish the existing HUD**, not build one from scratch. The current architecture (DOM HUD over WebGL canvas, authoritative server + jitter-buffered snapshot interpolation) already matches industry best practice. The real risk is **HUD state management at scale**: `main.js` is 2525 lines and mutates ~28 DOM nodes imperatively every frame — that is the seam to watch.

---

## 1. Current UI state of paper-war

### 1.1 Tech stack

- **No framework, no build step.** Vanilla JS ES modules served as static files by the Go server's `http.FileServer` (`server/cmd/server/main.go:28-48`). The only vendored library is [Motion](https://motion.dev/) (`client/vendor/motion.min.js`) for DOM animation, wired in via `client/src/motion_enhance.js` (`client/index.html:418-425`).
- **WebGL2 game canvas.** `client/src/gl.js` (1522 lines) implements two renderers: `SpriteBatch` (terrain/objects) and `InstancedBatch` using `gl.drawArraysInstanced` with GLSL `#version 300 es` shaders for units (`gl.js` class defs + `INSTANCE_FLOATS = 10`). Confirmed in the render path below.
- **DOM-overlay HUD.** All player-facing UI outside the game canvas is plain HTML/CSS in `client/index.html` + `client/style.css`.

### 1.2 Player-facing screens (`client/index.html`)

Six top-level screens toggled by the `.screen` / `.active` CSS classes:

| Screen | Lines | Purpose |
|---|---|---|
| Login | `index.html:17-27` | Name entry, joins server |
| Lobby (War Room) | `index.html:30-80` | Roster, commander pick, queue, career summary |
| Career | `index.html:83-100` | Cumulative cross-match stats |
| Leaderboard | `index.html:103-127` | Top players by kills |
| Clash config | `index.html:130-192` | Team size / commander / terrain picker |
| Game | `index.html:195-416` | The in-match HUD |

Screen switching is centralized in `App.showScreen(name)` (`client/src/app.js`), which removes `.active` from all `.screen` divs and adds it to the target — a manual but adequate router.

### 1.3 In-game HUD (`client/index.html:195-416`)

A classic RTS layout, all DOM except two canvases:

- **Top bar** (`:198-237`): player avatar/name, gold/units/bases/score resource cards, timer, mute + settings buttons.
- **Game viewport** (`:240-243`): `<canvas id="game-canvas">` (WebGL2) + a `.base-alert-overlay` div.
- **Bottom bar** (`:246-414`): selection panel, **minimap** (`<canvas id="minimap-canvas" width=120 height=180>`, a 2D-context canvas), and an action panel with commands, tactics, customizable tactic slots (`client/src/tactic_loadout.js`, localStorage-persisted), recruit buttons (7 unit types), and build buttons (3 structure types).

### 1.4 Render + HUD-update loop (`client/src/main.js`)

- **Loop** (`main.js:1234-1272`): `requestAnimationFrame`, throttled to ~30 fps (`frameInterval`). Order: `state.update` → `input.update` → `camera.update` → `particles.update` → periodic cleanup → `render()` → `updateUI()`.
- **Render** (`main.js:1278-1365`): a documented multi-pass pipeline over the WebGL renderer — terrain → fog → terrain objects → spawn markers → defensive structures → strongholds → units (Y-sorted) → HP bars → selection highlights → particles → selection box → minimap. This is the canonical "draw the world" split.
- **HUD update** (`main.js:2328-2373`, `updateUI()`): runs **every frame**, queries the DOM imperatively (`document.querySelector('#gold .resource-value')`, etc.) and sets `textContent` / toggles `.disabled` classes. ~28 `getElementById`/`querySelector` sites across the file. No diffing, no virtual DOM, no data binding.

### 1.5 Client ↔ server wire format

- **Transport:** WebSocket (`client/src/connection.js:76-138`). `binaryType = 'arraybuffer'`; two channels:
  - **JSON text messages** for control (login, join_queue, match_found, career_stats, leaderboard, start_solo, start_clash, reconnect). Dispatched in `App.handleServerMessage` (`client/src/app.js:291`).
  - **Binary** for hot-path data. Map terrain is prefixed `0xFF 0xFD` (`connection.js:126-131`). Snapshots are binary delta streams decoded in `state.js`.
- **Server side** (`server/cmd/server/main.go:128-145`): a `network.Hub` with two callbacks — a binary command dispatcher (translates clientID→playerID, forwards to `gs.HandleCommand`) and a text-message handler (the `login`/`join_queue`/`start_solo`/`start_clash`/`get_leaderboard` switch, e.g. `main.go:145-263`).
- **Persistence:** PostgreSQL if `DATABASE_URL` is set, else in-memory `MockStore` (`main.go:74-86`).

### 1.6 Netcode (already best-practice)

- **Authoritative server, 10 Hz tick** (`CONTEXT.md` "Tick" definition).
- **Client-side jitter-buffered interpolation** (`client/src/state.js:337-520`, ADR-0016): `applySnapshot` queues into a `pendingQueue` (capped at `MAX_PENDING_SNAPSHOTS`), `_activateSnapshot` shifts prev→curr, the render loop interpolates at 30 fps. Out-of-order/duplicate snapshots are rejected (`state.js:351-353`); a resurrect guard suppresses trailing snapshots for dead entities (`state.js:385-389`). This is exactly the Gaffer-on-Games pattern (see §3).
- **Reconnect** with 120 s tokens (ADR-0014, `main.go:94`, `connection.js:84-90`).

### 1.7 Design system

- **ADR-0021** adopted the `ui-ux-pro-max` skill output. Canonical doc: `docs/design-system.md` (204 lines) — semantic color tokens, Teko (display) + Inter (body) fonts, spacing scale, shadow depths, radii, animation tokens, WCAG-AA contrast verification, touch-target gates (≥44 px), and an a11y checklist. Pixel-verified against design mocks (`design/{component,main,map}.png`).
- **Procedural textures** generated by `scripts/gen-textures.py`: `parchment-tile.png`, `parchment-warm.png`, `wood-frame-9slice.png`, `navy-header-tile.png`, `gold-cta-tile.png`, `ink-border.png` — all in `client/assets/textures/`.
- **Separate a11y audit** at `docs/a11y-audit.md`.

### 1.8 Dev editor UIs (`client/editor/`)

Three internal tools, all deliberately **outside** the player design system (per the comment at `client/editor/units.html:8-10`):

- **units editor** (`units.html` + `units_editor.js`): edits the `CombatUnitTypeTable` + damage matrix, exports Go source to paste into `server/pkg/component/unit_type.go`. Includes an AI balance assistant (GLM/Claude backends). Plain dark CSS, scoped in the HTML `<style>` block.
- **map editor** (`map.html` + `map_editor.js`): 2D-canvas tile painter, mirror-X brush, undo via snapshot stack, connectivity check that rings spawn markers red when a movement profile can't path (`map_editor.js:229-260`, `:262-273`).
- **animation editor** (`animation.html` + `animation_editor.js`): 2D-canvas frame previewer with its own RAF loop (`animation_editor.js:420-429`).

---

## 2. Gaps — what "implement UI" most likely means here

Because the UI surface is broad, the ask is ambiguous. Ranked by how well the gap is supported by the codebase evidence:

1. **More / richer in-match HUD panels (most likely).** The bottom bar is built out but obvious RTS surfaces are absent: a settings modal (the `#settings-btn` at `index.html:235` has no backing modal), tooltips, an after-action report (AAR — `client/src/match_result.js` exists, so data is wired, but a results screen is not in `index.html`), economy/spend history, squad-inspector popover, "unit killed" / "base under attack" toasts (the `.base-alert-overlay` div exists at `index.html:242` but its content is thin).
2. **Refactor the imperative HUD layer (architecture debt).** `main.js` is 2525 lines; `updateUI()` re-queries the DOM every frame and mutates ~28 nodes. As panels multiply this becomes a maintenance and reflow-cost bottleneck. A small state-driven layer (or at minimum cached element refs + dirty checks) would help.
3. **A missing top-level screen.** E.g., a main menu / profile / armory / loadout-manager screen that sits between login and lobby. `client/src/tactic_loadout.js` already persists a loadout — a dedicated loadout editor screen is a plausible ask.
4. **Editor UI improvements.** The three dev tools are functional but visually divergent from each other and from the player UI. If the ask came while editing balance data, it may mean "make the units editor nicer."

**Recommend the user disambiguate between (1) and (2)** — both touch the same files, but (1) is additive (ship more panels) and (2) is structural (rebuild the HUD binding layer first so (1) is cheap). (3) and (4) are secondary.

---

## 3. Research findings (primary sources)

### 3.1 Hybrid DOM-overlay + WebGL canvas is the correct split

paper-war's architecture — WebGL for the world, DOM for the HUD — is the documented best practice for browser games. The DOM wins for **HUD layers specifically** on:

- **Accessibility** — native semantics, screen readers, keyboard focus, ARIA. A canvas-drawn button is an unlabeled pixel to assistive tech.
- **Text rendering** — crisp, font-hinted, DPI-correct for free.
- **Styling velocity** — CSS, design tokens (`docs/design-system.md`), and responsive layout are already solved problems.
- **Static / semi-static content** — resource counters, panels, menus rarely reflow at 60 fps, so DOM is often *faster* than canvas here.

Canvas/WebGL wins for the **dynamic game world** (hundreds of animating units per frame). paper-war already does this correctly: `InstancedBatch` + `drawArraysInstanced` in `gl.js` is the right tool for unit crowds, and the DOM HUD only updates the small set of changing fields each frame. Sources: [Pocket City — 5 Reasons to Use DOM Instead of Canvas for UI in HTML5 Games](https://blog.pocketcitygame.com/5-reasons-to-use-dom-instead-of-canvas-for-ui-in-html5-games/); [S. Lambert — HTML5 Game UI: Canvas vs DOM](https://blog.sklambert.com/html5-game-tutorial-game-ui-canvas-vs-dom/); [MDN — WebGL tutorial](https://developer.mozilla.org/en-US/docs/Web/API/WebGL_API/Tutorial).

**Caveat from the same sources:** DOM updates per frame should touch only what changed. `updateUI()` at `main.js:2328` re-queries selectors and writes `textContent` unconditionally every frame — the cost is fine today (~28 nodes) but is the first thing to refactor if the HUD grows.

### 3.2 Authoritative server + jitter-buffered snapshot interpolation is already right

Glenn Fiedler's *State Synchronization* lays out the exact pattern paper-war implements:

> "Network jitter exists. You don't have any guarantee that packets you sent nicely spaced out… arrive that way on the other side… To handle this situation you need to implement a **jitter buffer** for your state update packets. If you fail to do this you'll have a poor quality extrapolation and pops in stacks of objects…"

paper-war's `state.js:337-362` (`applySnapshot` + `pendingQueue` with `MAX_PENDING_SNAPSHOTS` cap and overflow-drop-oldest) is precisely this jitter buffer. The 10 Hz server tick vs 30 Hz client render rate gives the interpolation a ~100 ms delay budget — the standard trade of a little latency for smoothness. No action needed here; citing so the team knows the existing design is textbook-correct. Source: [Gaffer On Games — State Synchronization](https://www.gafferongames.com/post/state_synchronization/) (see also the companion [Snapshot Interpolation](https://www.gafferongames.com/post/snapshot_interpolation/) post).

### 3.3 DOM HUD at scale: move from imperative to data-bound, lightly

As HUDs grow past ~30–50 mutating elements, the "query the DOM every frame and set `textContent`" pattern starts to hurt both readability and (on low-end devices) layout cost. The options, lightest first:

1. **Cache element refs** in the constructor / `cacheDomRefs()` instead of re-querying every frame. Cheapest win; no architectural change.
2. **Dirty-check per field.** Only write `textContent` when the value actually changed (compare to a cached `last*` value). Eliminates most per-frame writes for slow-changing values like gold/score.
3. **Small state-driven HUD module.** Keep the rest of the app framework-free, but introduce a focused HUD layer that owns a `hudState` object and a `renderHud(state)` function, ideally with a tiny diff helper. This is the sweet spot for a vanilla-JS project — avoids pulling React/Lit/Svelte into a build-less pipeline.
4. **Full framework.** Only justified if the HUD becomes complex enough to outweigh the build-step cost. Given the project's explicit no-build, vanilla-JS stance, this is the last resort.

The consensus across the gamedev sources is that **for browser games with a build-less stack, a hand-rolled data-bound HUD layer is the norm**, not a framework. Sources: [SO — Canvas vs DOM in JS game dev](https://stackoverflow.com/questions/2266416/what-are-the-advantages-disadvantages-of-canvas-vs-dom-in-javascript-game-devel); [HTML5GameDevs — Canvas or DOM](https://www.html5gamedevs.com/topic/19597-canvas-or-dom-for-html5-games/).

### 3.4 Real-time control surface: keep input on the canvas, UI on the DOM

`client/src/input.js` handles mouse/keyboard for game commands (selection box, attack-ground, hotkeys Q/W/E/R, recruit/build). The established pattern (MDN's game-input guide) is to keep pointer/keyboard *gameplay* input on the canvas and route *UI* clicks through normal DOM event handlers on overlay elements. paper-war already does this — `index.html` buttons are real `<button>` elements with `click` handlers, which gives keyboard focus, Enter-to-activate, and a11y for free. Touch targets should stay ≥44 px (already a rule in `docs/design-system.md` "Touch targets"). Source: [MDN — Mobile touch controls / Game control mechanisms](https://developer.mozilla.org/en-US/docs/Games/Techniques/Control_mechanisms/Mobile_touch).

---

## 4. Recommended approach

Given the survey, the recommended path — **assuming interpretation (1)+(2)** (extend the HUD, and refactor the binding layer so extension stays cheap):

1. **Extract a HUD module.** Move the imperative `updateUI` body out of `main.js` into `client/src/hud.js`. Constructor caches element refs once; `update(snapshot)` writes only changed fields. This unblocks every subsequent panel addition and shrinks `main.js` meaningfully.
2. **Add the missing settings modal** behind `#settings-btn` (`index.html:235`) — it's currently a dead button and is the lowest-friction visible "UI" win.
3. **Add an AAR / match-results screen** as a new `.screen` div. `client/src/match_result.js` already produces the data; it just needs a DOM surface (ADR-0013 references match statistics, so this is ADR-aligned).
4. **Wire event toasts** into the existing `.base-alert-overlay` (`index.html:242`) for "base under attack" / "commander killed" — the overlay exists but is underused.
5. **Keep the editors as-is** (deliberately outside the design system per `units.html:8-10`); only revisit if the user explicitly meant editor UX.

If the user actually meant interpretation (3) or (4), the research above still applies — just scope the screen/editor accordingly. **Do not** introduce a build step or framework unless the user confirms; the vanilla-JS, file-server-served stack is intentional and the HUD layer above keeps it viable.

---

## 5. Sources

| Source | URL | Used for |
|---|---|---|
| Gaffer On Games — State Synchronization | https://www.gafferongames.com/post/state_synchronization/ | Validating the existing jitter-buffer + snapshot-interpolation netcode (§3.2) |
| Gaffer On Games — Snapshot Interpolation | https://www.gafferongames.com/post/snapshot_interpolation/ | Companion reference for the 10 Hz→30 fps interpolation trade |
| Pocket City — 5 Reasons to Use DOM Instead of Canvas for UI in HTML5 Games | https://blog.pocketcitygame.com/5-reasons-to-use-dom-instead-of-canvas-for-ui-in-html5-games/ | DOM-overlay vs canvas tradeoffs, a11y, text rendering (§3.1) |
| S. Lambert — HTML5 Game Tutorial: Game UI Canvas vs DOM | https://blog.sklambert.com/html5-game-tutorial-game-ui-canvas-vs-dom/ | Hybrid HUD approach, scaling considerations (§3.1) |
| MDN — WebGL Tutorial | https://developer.mozilla.org/en-US/docs/Web/API/WebGL_API/Tutorial | WebGL/canvas fundamentals, compositing with HTML (§3.1) |
| MDN — Game control mechanisms / Mobile touch | https://developer.mozilla.org/en-US/docs/Games/Techniques/Control_mechanisms/Mobile_touch | Input routing (canvas vs DOM) and touch-target sizing (§3.4) |
| StackOverflow — Canvas vs DOM in JS game dev | https://stackoverflow.com/questions/2266416/what-are-the-advantages-disadvantages-of-canvas-vs-dom-in-javascript-game-devel | Community consensus on HUD at scale (§3.3) |
| HTML5GameDevs — Canvas or DOM for HTML5 games | https://www.html5gamedevs.com/topic/19597-canvas-or-dom-for-html5-games/ | Forum consensus on build-less HUD patterns (§3.3) |
| Project source — `client/src/{main,app,state,connection,gl}.js`, `client/index.html`, `server/cmd/server/main.go` | (repo) | §1 survey — all code citations are file:line in those files |
| Project docs — `docs/design-system.md`, `docs/a11y-audit.md`, ADRs 0013/0014/0016/0021, `CONTEXT.md` | (repo) | Design system, netcode, and screen-surface grounding (§1.6, §1.7, §3.4) |
