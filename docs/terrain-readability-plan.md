# Terrain Validation & Readability — Milestone Plan

Goal: the `terrain-starcraft-plan.md` simulation is fully shipped (Phases 1–4
are code-complete), but **(a)** its own acceptance criteria have never been
validated end-to-end, and **(b)** none of the simulated rules are visible to
the player. This milestone closes both gaps.

Framing: **depth-first on terrain** (animation #2 and UI/OpenPencil #3 are
deferred to a later milestone; ADR-0032 stays on hold). Two phases.

Continues `terrain-starcraft-plan.md`; does not re-litigate its Phase 1–4
decisions (notably the Phase 2 LOS simplification: height-aware, **not** true
StarCraft cliff-edge blindness).

---

## Shared decisions (resolved — do not re-litigate)

- **Validation bar**: fix anything that would make a readability cue *lie*; punt
  non-blocking issues to the tracker.
- **Readability cues computed client-side** from the elevation texture (CPU
  mirror of `u_elevationTex`) + a hardcoded 7-type Range table, drift-guarded.
  No network snapshot change for Range.
- **Cliff LOS stays height-aware** (`fog.go:149-160`) — the Phase 2 deliberate
  simplification. True cliff-edge blindness is out of scope.
- **Glossary already updated this session**: `Elevation` corrected (was stale
  "visual-only"), `Ramp` added to the Terrain Type list, `Cliff` and `Creep`
  entries added (`CONTEXT.md`).

---

## Phase 0 — Validate: meet the terrain plan's unmet acceptance criteria

The plan's acceptance ("a clash on ClashHills: armies route via ramps,
high-ground units shoot further, doodads destroyable, creep spreads") is
currently aspirational — there is **no** end-to-end terrain integration test,
and the harness cannot see terrain at all.

1. **`ClashHills` clash-map asset** — terrain-rich: 2-tier cliffs, Ramp
   crossings, destructible doodads (Rock/Forest/Wall with HP), creep sources.
   Author via `client/editor/map_editor.js`; register in `LoadClashMap`
   (`server/pkg/tilemap/clash_maps.go`). Verify spawns stay connected.
2. **GameSession integration tests** (`server/pkg/game/`), one per behavior —
   these are the gate, not isolated flowfield/fog tests:
   - **Cliff→ramp routing**: a unit's flow field routes around a 2-tier cliff
     via Ramp through a real `gs.Tick()` loop.
   - **High-ground +1 range**: `attackerElev > targetElev` fires the bonus
     through a full session (`combat.go:222-235`), not just in isolation.
   - **Creep spread**: `CreepSystem` spreads from an owned source over N ticks;
     a friendly unit pays the ×0.7 cost on a creep tile.
   - **Doodad conversion**: damage to a destructible doodad →
     `terrainSys.ProcessDestruction` → terrain converts + an
     `EventTerrainChange` is emitted.
3. **Instrument the playtest harness** (`playtest_matrix_test.go`): add per-tick
   terrain telemetry (ramp-routing events, high-ground bonus applications, creep
   tile count, doodad conversions) and run on `ClashHills` alongside `plains`,
   so ongoing balance runs cover terrain.
4. **Fix blockers** per the validation bar: any behavior that doesn't fire live
   must be fixed before Phase 1 (a range ring on a broken bonus is a lie).

Gate: integration tests green; harness reports terrain telemetry on ClashHills.

---

## Phase 1 — Readability: make the rules visible (flagship cues only)

1. **High-ground range ring** (client) — on unit select, draw effective range =
   `baseRange + (attackerElev − targetElev)` (+1 tile per level).
   - CPU mirror of the `u_elevationTex` `Uint8Array` at its upload site
     (`client/src/gl.js:1099-1128`); `elevBand = mirror[floor(renderY)*w + floor(renderX)]`.
   - Hardcoded 7-type Range table mirroring `server/pkg/component/unit_type.go:59-95`,
     with a client-side drift-guard test (fail if it diverges from the server
     constants). Mirrors the existing client pattern of duplicating server
     constants (`TERRAIN_COLORS`, `PROFILE_COSTS`).
   - New ring geometry (triangle-fan / line-loop) on the effects pass
     (`renderer.drawEffects`, `client/src/main.js:1394`, `gl.js:1397`) — there
     is no existing circle primitive to extend.
2. **Cliff/ramp legibility** (client shader) — make 2-tier cliffs read as walls
   and Ramps as doors in the elevation-aware shading (`gl.js:196-252`), so a
   player sees impassability without a tooltip.

Gate: manual clash on `ClashHills` — a player can read high-ground advantage at
a glance (range ring extends with elevation) and see where they can/can't walk.

---

## Out of scope (deferred)

- **Creep player-facing clarity cue** — creep is observable to the dev via the
  Phase 0 harness; the player-facing cue is a follow-on.
- **Elevation overlay toggle** — Phase 2 readability, follow-on milestone.
- **LOS/vision preview** — heaviest cue (client must replicate the LOS
  algorithm); deferred indefinitely.
- **Deeper simulation** — true cliff-edge LOS, 4th elevation band, multi-tile
  cliff faces. Would re-litigate Phase 2's documented simplification.
- **#2 Animation, #3 UI/OpenPencil** — deferred per the depth-first decision.
  ADR-0032 (OpenPencil as UI source-of-truth) stays on hold until UI returns to
  the foreground.

---

## Acceptance (whole milestone)

- The original terrain plan's acceptance now **passes as integration tests** on
  `ClashHills`, not as aspiration.
- The playtest harness reports terrain telemetry when run on `ClashHills`.
- In a clash, selecting a unit on high ground shows a range ring that extends
  with elevation advantage; 2-tier cliffs read as impassable walls.
- No existing test regresses; new integration tests are CI-gated.

## Navigation index (load-bearing file:line)

- Terrain plan (predecessor): `docs/terrain-starcraft-plan.md`
- Playtest harness: `server/pkg/game/playtest_matrix_test.go:18-26,55-67`
- High-ground bonus: `server/pkg/combat/combat.go:222-235` (isolated test
  `server/pkg/combat/combat_test.go:306-340`)
- Creep: `server/pkg/creep/creep.go`, `server/pkg/game/session.go:193,2133`
- Cliff pathfinding rule: `server/pkg/pathfinding/flowfield.go:56-63`
- LOS (height-aware): `server/pkg/fog/fog.go:149-160`
- Client elevation texture: `client/src/gl.js:73,1099-1128`
- Client unit descriptors: `client/src/main.js:1948-2187`; selection highlights
  `:2157`; effects pass `:1394`
- Range stat (server): `server/pkg/component/unit_type.go:45,59-95`
- Clash map registry: `server/pkg/tilemap/clash_maps.go`; editor
  `client/editor/map_editor.js`
