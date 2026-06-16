# Plan: Paper War v2 — Player Control & Visual Glow-Up

**Status**: Draft (generated via `/plan` after user asked "how about v2 plan?" and did not respond to scope clarification — direction chosen per user's established preferences: decisive, keeps options open, defers complex bets, fights for core-vision features, ships in ready increments)
**Date**: 2026-06-14
**Theme**: "Make it look and feel like a real game" — expose the depth v1's engine already supports, and close the visual gap to the design north star (`design/main.png`, `design/component.png`)
**Rationale**: Reconnaissance shows v1 cut a lot of features that are **already half-built in code** — formations have a full server implementation + tests, the GL renderer is already a sprite-batch engine using a 1x1 white placeholder texture, HP bars already render. v2's job is to **finish and expose** these, not build from scratch. The heavy bets (PvP netcode, multi-squad, morale, air units) defer to v3.

---

## Goal

Ship a v2 that is **visibly and tangibly closer to the `design/main.png` north star**, gives players **meaningful new control** (formations, map choice), and **polishes the UX** (HUD, lobby, small wins) — all without a major architecture change. A player loading v2 should immediately notice: real unit sprites instead of colored rectangles, a richer HUD, the ability to switch formations mid-match, and the ability to pick a map.

**Definition of Done for v2**:
- Unit sprites render from a texture atlas (no more 1x1 white placeholder)
- Players can cycle formations (Line/Wedge/Circle/Scatter) mid-match via hotkey, and units re-form accordingly
- Players can pick a map (preset or seed) from the lobby instead of getting a random one
- HUD matches the `design/main.png` layout (gold, roster summary, objective status, mini-map if present in design)
- All v1 tests stay green; new tests cover formation switching + map selection
- A new ADR documents the v2 visual/atlas decision
- Zero regressions in the crash-restart / reconnect flows fixed in #17

## Current context / assumptions

- **Repo**: `/Users/zhangrong/repo/paper-war` (GitHub `zhangr011/paper-war`, `master`, direct-commit)
- **Server**: Go. `cd server && PORT=9091 go run ./cmd/server`. Dev mode = in-memory MockStore.
- **Client**: raw JS, no build step. Browser global `window.__paperWarApp`.
- **Existing tests**: 51 server `*_test.go`, 23 client tests (3 suites), 4 Playwright e2e specs (fog/map only). Crash-restart regression spec pending from the v1 QA plan.
- **Issue tracker**: GitHub Issues via `gh`. Labels: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`.
- **Browser vision tooling**: `browser_vision` fails on current provider (zai/glm-5.1, 429). Visual QA falls back to canvas pixel-sampling via `browser_console` IIFEs. This affects how we verify sprite rendering.

### Key reconnaissance findings (what's already built)

| Feature | Status | Evidence |
|---------|--------|----------|
| **Formation system** | Server DONE, client NOT exposed | `server/pkg/formation/formation.go` implements Line/Wedge/Circle/Scatter via `CalcOffsets()`. `formation_test.go` covers all four. `component/formation.go` has `FormationType` field. But no client UI sends formation changes; squad formation is fixed at default. |
| **Sprite batch renderer** | DONE, using placeholder texture | `client/src/gl.js` is a WebGL2 instanced sprite batch with texture-atlas support. Comment at line 199: *"1x1 white pixel texture used as a placeholder until a real texture atlas is loaded. Makes every draw render as a colored quad."* Loading a real atlas flips colored quads into real sprites with no renderer rewrite. |
| **HP bars** | DONE | `main.js:968` calls `renderer.drawHPBars(unitDescs, cameraOffset)`. `.scratch/issues/02-show-combat-unit-hp.md` is effectively obsolete. |
| **Tactical orders** | DONE | `Follow/Charge/Retreat/Hold` wired through `input.js:190` → `connection.js:235` (`CmdTacticalOrder 0x05`). Keys 1-4. |
| **Map generation** | Seed-based, no picker | `tilemap/generate.go:61 GenerateMap(w, h, seed)`. Server picks randomly; client has no control. Exposing a seed/preset picker is a small change. |
| **Textured terrain** | DONE (commit 6f5b44f, #15) | Fragment-shader noise, dark earthy palette, animated water. |
| **Audio** | DONE (commit a7501e0, #12) | Combat SFX, UI feedback, ambient, match stingers. |
| **Reconnect** | DONE + recently fixed (commits f63cb12, 8fff54c, 5e0d467) | Token-based for solo/PvP; `cleanupGame()` path for clash (no token). |

### What v1 explicitly cut (the v2/v3 backlog)

From `.scratch/v1-design-overhaul/PRD.md` "Out of Scope":
- ❌ Multiplayer (PvP) — **defer to v3** (massive netcode/matchmaking/accounts bet)
- ✅ Sound effects — done in #12
- ✅ Sprites/animated graphics — **v2 picks this up** (renderer is ready)
- ❌ Morale system — defer to v3
- ❌ Formation switching — **v2 picks this up** (server is ready)
- ❌ Multiple Squads per player — defer to v3
- ❌ Map selection — **v2 picks this up** (small effort)
- ❌ Account system — defer to v3 (tied to PvP)
- ❌ Matchmaking — defer to v3 (tied to PvP)
- ❌ Air units — defer to v3
- ❌ Server clustering — defer indefinitely

---

## Proposed approach

Five workstreams. WS1 (formations) and WS4 (map selection) are the cheapest high-impact wins because the server-side is done. WS2 (sprite atlas) is the headline visual change and the largest single item, but the renderer is ready so it's mostly art + atlas loading. WS3 (HUD) and WS5 (small UX) round out the "feels like a real game" story.

Order by dependency + value:
1. **WS1 — Formation switching** (unlocks tactical depth, cheap, server done)
2. **WS4 — Map selection** (cheap, player agency, unblocks WS3 lobby redesign)
3. **WS2 — Sprite atlas + unit sprites** (headline visual, needs art assets — start early in parallel)
4. **WS3 — HUD & lobby overhaul toward design/main.png** (depends on WS2 for final look, WS4 for lobby layout)
5. **WS5 — Small UX wins** (filler, ship-blocker-free)

Art asset creation (WS2) is the long pole and should start on day 1, in parallel with WS1/WS4 code work.

---

## Step-by-step plan

### WS1 — Formation switching [~1-2 days code]

**Goal**: Let players cycle Line/Wedge/Circle/Scatter mid-match; units re-form to the new offsets.

1. **Add network command** `CmdSetFormation` (new opcode, e.g. `0x06`):
   - `client/src/connection.js` — add `sendSetFormation(squadID, formationType)` mirroring `sendTacticalOrder` (line 235). Payload: header + squadID(uint32) + formationType(uint8) = 15 bytes.
   - `server/pkg/network/protocol.go` — register the new opcode + decoder.

2. **Server handler** in `server/cmd/server/main.go` (near the `start_clash`/`start_solo` handlers):
   - On `CmdSetFormation`, update the squad's `FormationComponent.FormationType` on the targeted entity.
   - The existing MovementSystem + `formation.CalcOffsets()` already consume `FormationType`, so the next tick re-forms the squad automatically. Verify this — if MovementSystem caches offsets, add a recompute trigger.

3. **Client input**:
   - `client/src/input.js` — add a hotkey (suggest **F** to cycle, or **Shift+1/2/3/4** for direct select to avoid clashing with tactical orders on 1-4). Cycle is simpler for v2.
   - `client/src/main.js` — on formation key, call `connection.sendSetFormation(this.squadID, nextType)`. Show a transient toast/indicator of the new formation.

4. **Client rendering cue** (optional but high-value for feedback):
   - Draw the formation outline faintly under the squad (faint line/wedge/circle silhouette). Uses existing renderer primitives. Skip if time-constrained.

5. **Tests**:
   - Server: `server/pkg/formation/formation_test.go` already covers offset calculation. Add a test in `server/pkg/network/` or a new `formation_handler_test.go` that a `CmdSetFormation` message updates the squad's FormationType and the next MovementSystem tick repositions units.
   - Client: add to `client/src/state_test.mjs` — verify formation cycle logic (Line→Wedge→Circle→Scatter→Line).
   - E2E (optional): extend the crash-restart spec or add a new Playwright spec that cycles formations and asserts unit positions change.

6. **Files likely to change**:
   - `client/src/connection.js` (new send method)
   - `client/src/input.js` (hotkey)
   - `client/src/main.js` (handler + toast)
   - `server/cmd/server/main.go` (handler)
   - `server/pkg/network/protocol.go` (opcode)
   - `server/pkg/formation/formation_test.go` or new test file (tests)

7. **Verification**: `go test ./pkg/formation/... ./pkg/network/...` green; `node client/src/state_test.mjs` green; manual browser test cycling all 4 formations in a solo match.

### WS4 — Map selection [~0.5-1 day code]

**Goal**: Player picks a map (preset or seed) from the lobby instead of getting a random one.

1. **Define map presets** — a small curated set (e.g. 4-6 presets) with names and seed values. Suggest:
   - "Crossroads" (open, road-heavy)
   - "River delta" (water-heavy, bridge-focused)
   - "Highlands" (elevation, chokepoints)
   - "Forest ambush" (dense terrain)
   - Plus a "Random" option (current behavior) and maybe "Custom seed" (text input)
   - Seeds: pick stable seeds that generate good maps (validated via existing `tilemap/fixture_test.go` patterns).

2. **Lobby UI** (`client/index.html` + `client/style.css`):
   - Add a map picker section near the commander-select area. Dropdown or card grid.
   - Default to "Random" to preserve current behavior.

3. **Client → server**: extend the `start_solo` / `start_clash` message with an optional `mapSeed` (int64) or `presetName` (string). Backward-compatible — missing field = random.

4. **Server handler** (`main.go` start handlers):
   - If `mapSeed` provided and valid, use it: `tilemap.GenerateMap(w, h, seed)`.
   - If `presetName` provided, resolve to its seed.
   - Validate the seed produces a connected map (the generator already retries on connectivity failure — confirm a fixed seed is deterministic and stable).

5. **Files likely to change**:
   - `client/index.html` (lobby UI)
   - `client/src/app.js` (lobby logic — track selected map, send in start message)
   - `client/src/connection.js` (extend start message encoding)
   - `server/cmd/server/main.go` (parse map selection, pass seed)
   - `server/pkg/tilemap/presets.go` (NEW — preset definitions, or inline in main.go)

6. **Tests**:
   - Server: test that a provided seed produces a deterministic map; test preset resolution.
   - Client: lobby state tracks selected preset.

7. **Verification**: pick each preset from lobby, start match, confirm map differs; "Random" still works.

### WS2 — Sprite atlas + unit sprites [~3-5 days, art is the long pole]

**Goal**: Replace the 1x1 white placeholder texture with a real sprite atlas so units render as the shapes/icons in `design/component.png` instead of flat colored quads.

**This is the headline visual change of v2 and the biggest single item.** The renderer (`gl.js`) already supports it — this is primarily an art asset + integration task.

1. **Study the design reference** — open `design/component.png` (1.5MB) and `design/main.png` (1.9MB). Extract the canonical look for each of the 7 CombatUnitTypes + Commander. Note: these are large images — use `vision_analyze` if the provider supports it, else sample regions via an image tool. Document the intended look per unit type in a short `docs/v2-sprite-spec.md`.

2. **Create the sprite atlas** (the art long pole):
   - One atlas PNG containing all unit sprites + Commander variant + terrain objects if upgrading those too.
   - Layout: grid of sprites, documented coordinates for `a_spriteOffset`/`a_spriteSize` in the existing instanced renderer.
   - Style: match the paper/pencil aesthetic established by the textured terrain shader (dark earthy palette, per ADR-0015). Not realistic — stylized.
   - Per-type sprites needed (7 CombatUnitTypes × maybe 8 directions? or simpler: top-down single orientation with rotation in shader): Light Infantry, Heavy Infantry, Sniper, Anti-Armor Infantry, Motor Gun, Motor Artillery, Motor Missile. Plus Commander overlay (white border already renders — decide if sprite includes border or keeps shader-based border).
   - **Direction question**: v1 renders top-down colored shapes with no facing. Sprites can be (a) single top-down icon per type (cheapest), (b) directional variants (8-way, more art), or (c) single icon rotated by facing (medium). Recommend (a) for v2, (c) as a v2.1 polish. Flag as an open question.

3. **Load the atlas** in `gl.js`:
   - Replace the `createPlaceholderTexture()` call (line ~199) with real `loadTexture('assets/sprites.png')` via `HTMLImageElement` → `texImage2D`.
   - Wire up the atlas coordinate uniforms (`u_atlasSize`) and per-instance `a_spriteOffset`/`a_spriteSize` attributes (already declared in the instanced shader, lines 125-126 — confirm they're being fed).
   - Update `main.js` unit-description builder to pass the correct atlas offset per `unitType`.

4. **Asset pipeline**: since the client has no build step, the atlas PNG goes in `client/assets/sprites.png` and is fetched at runtime. Add to the server's static file serving (already serves `client/*`).

5. **Animation** (optional v2.1, flag for v3 if time-constrained):
   - The shader already supports animation via `u_time` (water animation uses it). A simple bob/march animation per unit is feasible but not required for v2 ship.

6. **Files likely to change**:
   - `client/assets/sprites.png` (NEW — the atlas; art asset)
   - `client/src/gl.js` (atlas loading + coordinate wiring)
   - `client/src/main.js` (per-unit-type atlas offsets)
   - `docs/v2-sprite-spec.md` (NEW — documents the look + atlas layout)
   - `server/cmd/server/main.go` (serve `assets/` if not already covered by the static handler)

7. **Tests**:
   - Client: `client/src/state_test.mjs` — verify unit-type → atlas-offset mapping function.
   - Visual: manual + canvas pixel-sampling via `browser_console` (since `browser_vision` is broken on current provider). Sample pixels at known unit positions and assert they're not all-white (the placeholder) anymore.

8. **Verification**: load a match, confirm units render as sprites matching the spec, not white quads; HP bars + commander border still render correctly on top.

9. **ADR**: write `docs/adr/0017-sprite-atlas-v2.md` documenting the atlas decision, direction-handling choice, and why the renderer needed no rewrite.

### WS3 — HUD & lobby overhaul [~2-3 days]

**Goal**: Bring the in-match HUD and lobby closer to `design/main.png`. Depends on WS2 (final sprite look) and WS4 (lobby layout includes map picker).

1. **Study `design/main.png`** — extract the target HUD layout: gold position, roster/summary panel, objective indicator, mini-map (if present), recruit/build button placement, formation indicator (ties to WS1).

2. **HUD changes** (`client/index.html` + `style.css` + `main.js` HUD update section ~line 1646):
   - Restructure the gold/resource display to match design.
   - Add an objective status indicator (current objective + progress — e.g. capture tug-of-war bar, survival timer).
   - Add a formation indicator showing current formation (ties to WS1).
   - Mini-map: if `design/main.png` shows one, this is a v2 stretch goal — the data exists (terrain + fog + unit positions), but rendering a mini-map is non-trivial. Flag as optional; if cut, file as v2.1.

3. **Lobby changes**:
   - Integrate the map picker (WS4) into the layout.
   - Improve roster display (already functional — polish to match design).

4. **Match-end / AAR screen** (already exists per #13) — polish to match design if it diverges.

5. **Files likely to change**: `client/index.html`, `client/style.css`, `client/src/main.js` (HUD update), `client/src/app.js` (lobby).

6. **Verification**: visual side-by-side with `design/main.png`; manual walkthrough of all screens.

### WS5 — Small UX wins [~0.5-1 day]

**Goal**: Clear the cheap items from `.scratch/issues/` that fit the v2 theme.

1. **`.scratch/issues/01-clash-test-team-size-color.md`** — clash test team size/color improvements. Read the issue, implement if still relevant.
2. **`.scratch/issues/03-terrain-select-clash-test.md`** — terrain select for clash test. Read, implement if relevant (may overlap with WS4 map selection — dedupe).
3. **`.scratch/issues/02-show-combat-unit-hp.md`** — **likely obsolete** (HP bars already render per `main.js:968`). Verify and close/archive.
4. **Any small bugs** found during the v1 QA pass (see companion QA plan) that are quick fixes.

Files: varies. Verification: existing tests stay green.

---

## Cross-cutting work

- **ADR-0017** (sprite atlas) — required. Additional ADRs only if a non-obvious decision arises (e.g. formation hotkey scheme, map preset validation strategy).
- **Issues**: each workstream gets a tracking issue in GitHub with the `ready-for-agent` or `ready-for-human` label depending on whether it's art (human) or code (agent-doable).
- **Testing baseline**: before starting, run the v1 QA pass plan (`.hermes/plans/2026-06-14_063757-v1-qa-hardening-pass.md`) to establish a green baseline. v2 must not regress v1.
- **Commits**: direct to `master` per established pattern. Each workstream = a feature commit (or a small set).

## Files likely to change (summary)

| WS | File | Change |
|----|------|--------|
| 1 | `client/src/connection.js`, `input.js`, `main.js` | Formation send/hotkey/feedback |
| 1 | `server/cmd/server/main.go`, `pkg/network/protocol.go` | Formation command handler + opcode |
| 4 | `client/index.html`, `src/app.js` | Map picker UI + logic |
| 4 | `server/cmd/server/main.go`, `pkg/tilemap/presets.go` (new) | Seed/preset handling |
| 2 | `client/assets/sprites.png` (new) | The atlas (art) |
| 2 | `client/src/gl.js`, `main.js` | Atlas loading + per-type offsets |
| 2 | `docs/v2-sprite-spec.md`, `docs/adr/0017-*.md` (new) | Spec + ADR |
| 3 | `client/index.html`, `style.css`, `src/main.js` | HUD + lobby overhaul |
| 5 | varies | Small UX wins |

## Tests / validation

- **Per workstream**: `cd server && go test ./...` (51+ files) and `node client/src/*_test.mjs` (23+ tests) must stay green.
- **Per workstream**: `npx playwright test` — must stay green; add specs for formation switching + map selection.
- **WS2 visual**: canvas pixel-sampling via `browser_console` (provider limitation workaround) — assert units render non-white pixels at known positions.
- **Regression**: re-run the crash-restart spec (from the v1 QA plan) after every WS merge — the #17 fix must not break.
- **Final gate**: full manual walkthrough — login → lobby (pick map + commander) → solo match (cycle formations, see sprites, read HUD) → match-end → another match. No reload between. Then the same in clash mode.

## Risks, tradeoffs, open questions

- **Risk — art asset availability**: WS2 depends on a sprite atlas matching the paper/pencil aesthetic. If no art resource is available, options are: (a) generate procedural sprites via shader (cheap but may not match design), (b) use simple iconography (Unicode/shape-based atlas), (c) defer WS2 to v2.1 and ship WS1+WS3+WS4+WS5 as a smaller v2. **This is the biggest open question — flag to user.**
- **Risk — formation hotkey collision**: tactical orders use 1-4; formations need a different scheme. Recommend **F** to cycle, or a modifier (Shift+1-4). Open question.
- **Risk — sprite direction handling**: single top-down icon vs 8-way vs shader-rotated. Recommend single icon for v2 (cheapest), rotation for v2.1. Open question.
- **Risk — mini-map scope creep**: if `design/main.png` shows a mini-map, it could swallow WS3's budget. Recommend deferring mini-map to v2.1 unless it's trivial.
- **Tradeoff — deferring PvP to v3**: keeps v2 shippable and avoids a netcode rewrite, but means v2 is still single-player-vs-AI. The user's profile ("keeps options open", "ship-ready") supports this, but it's a real strategic choice — flag for confirmation.
- **Open question — does MovementSystem cache formation offsets?** If yes, WS1 needs a cache-invalidation path when FormationType changes. Verify early in WS1.
- **Open question — are the `design/*.png` files the authoritative visual target, or aspirational reference?** Determines how literally WS2/WS3 should match them.

## Out of scope (v3+)

- PvP multiplayer, matchmaking, accounts
- Multiple squads per player
- Morale system
- Air units
- Server clustering / horizontal scaling
- Mini-map (likely v2.1)
- Sprite animation / directional facing (likely v2.1)
- Anything cosmetic below the "feels like a real game" bar

---

## Execution order

1. **Establish green baseline** — run the v1 QA pass plan first; fix any criticals found. v2 builds on a stable v1.
2. **WS1 (formations)** + **WS4 (map selection)** in parallel — both cheap, server-side largely done, high player value. Ship these first as a quick "v2.0-alpha".
3. **WS2 (sprite atlas)** — start art asset creation on day 1 (long pole); integrate into `gl.js` when assets land.
4. **WS3 (HUD/lobby)** — after WS2 (so the look is final) and WS4 (so lobby layout is settled).
5. **WS5 (small UX)** — filler between major workstreams; clear whenever a blocking dependency pauses WS2/WS3.
6. **Final integration test** — full manual walkthrough + all automated tests green + crash-restart regression green.
7. **ADRs + issues closed + version bump** — declare v2 done.

---

*This plan was generated in plan mode. No code or project files were modified in its creation (only this plan file was written). Execution requires exiting plan mode. The single biggest open question — art asset availability for WS2 — should be confirmed before committing to the full v2 scope; if art is unavailable, WS1+WS3+WS4+WS5 ship as a smaller v2 and WS2 becomes v2.1.*
