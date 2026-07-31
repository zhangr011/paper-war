# Plan — Terrain Polish (Stronghold Building Rendering + Trees)

Finishes two visual surfaces left open by accepted ADRs, plus decorative tree
coverage on hills. No gameplay, cover, LOS, or balance change — pure rendering
and a small wire-format widening.

Driven by a grill-with-docs session (2026-07-31). Every branch below was
resolved against the existing domain model; see "Decision rationale" at the end.

## Scope

1. **Stronghold → Building rendering** — the open client consequence of
   ADR-0023. The entity path already exists (`buildStrongholdDescriptors`,
   Pass 2.7); this enriches the sprite and removes the dead pre-ADR-0023
   terrain-icon branch. Adds HP + garrison fields to `StrongholdState`.
2. **Richer tree sprites** — replace the flat canopy rect with a shaped
   canopy, hashed size + variety. Same density, no mechanic change (stays
   inside ADR-0024's "environment = terrain").
3. **Decoration trees on hills** — cosmetic Pass-2 sprites on Hill tiles,
   sparse, favoring slopes. Hill mechanics untouched.

## Out of scope (deliberately deferred)

- Edge-blend / coastline feathering between terrain types (shader rework).
- `TERRAIN_COLORS` palette tuning.
- Generator presence / distinct floors for underused types (Road, Swamp,
  Snow, Desert).
- Elevation shading relief (ADR says elevation is visual-only; current
  `hillShadeRGB` layer tinting stays as-is).
- Any change to cover, LOS, or movement cost — terrain stays the single
  source of truth (ADR-0024).
- Network snapshot changes beyond the three new `StrongholdState` fields.

## Edits

### 1. Stronghold wire format — `server/pkg/game/session.go`

`StrongholdStates()` (`session.go:529`) currently emits `{X, Y, Level, Faction}`
only. Widen `StrongholdState` with three fields, all sourced from pools already
held in that function:

- `HP` — from `hpPool.HP` (the `hp.HP > 0` guard already exists).
- `MaxHP` — `component.StrongholdHP(sc.Level)` (same formula used at spawn,
  `session.go:592`). Sent so the client needn't replicate the formula.
- `Garrison` — current garrison count, from `StrongholdComponent` (the field
  that tracks occupied slots; verify exact accessor in
  `server/pkg/component/stronghold.go`).

`Capacity` is **not** sent — client derives it from `Level` via
`component.StrongholdCapacity(level)` (mirrors the spawn formula at
`session.go:597`). Add a client-side `strongholdCapacity(level)` helper to keep
the formula in one server import path (or hardcode the small table — see
Decision 3 below).

Dedup (`StrongholdStateIfChanged`, `session.go:517`) is JSON-marshal diffing,
so the wider struct broadcasts only when a field changes. Strongholds are
1–5 per map, so per-tick HP churn during a siege is negligible.

Client parse: extend the `stronghold_state` handler in `client/src/app.js:317`
and the field list in `client/src/main.js:265` to carry the new fields.

### 2. Stronghold sprite — `client/src/main.js`

In `buildStrongholdDescriptors` (`main.js:1659`), keep the existing
faction-colored keep + dark battlement backing, then add:

- **HP bar** — thin bar above the keep, faction-tinted fill against a dark
  track, width = `HP / MaxHP`. Mirrors the unit HP-bar convention (recent
  commits `9c10f30`, `14c439b`).
- **Garrison pips** — `Garrison` filled pips out of `strongholdCapacity(level)`
  total, drawn under the keep. Empty pip = dark, filled = faction color.

Faction color and level-scale (already present) are unchanged.

### 3. Delete dead terrain-id stronghold branch — `client/src/main.js`

Remove `buildTerrainObjects`'s `else if (terrainType >= 11 && terrainType <= 15)`
block (`main.js:1520-1548`). Terrain ids 11–15 are reserved/retired
(`server/pkg/component/movement.go:17`); the branch is unreachable. Also prune
the now-stale "stronghold icons" mention in the `buildTerrainObjects` doc comment
(`main.js:1458`).

### 4. Richer tree sprites — `client/src/main.js`

In `buildTerrainObjects`'s Forest branch (`main.js:1483-1519`):

- Replace the flat canopy rect with a shaped canopy — triangle (pine) or
  rounded stack (broadleaf), selected by the existing per-tile hash so it is
  deterministic and flicker-free.
- Add hashed size variation within a small band.
- Keep trunk rendering; keep the 1–3-trees-per-tile count (`hash1 % 3 + 1`).

No change to `applyForest` density, `forestFraction`, or Forest's
cover/LOS/cost.

### 5. Decoration trees on hills — `client/src/main.js`

In `buildTerrainObjects`, add a Hill branch (terrain type 5) that, by hash and
at low probability (~15–20% of slope tiles), draws a single decoration tree.
Use `elevationData` (`main.js:263,355`) to favor layer 1 (slope) over layer 2
(peak) — peaks stay bare so they read as high ground. These are pure Pass-2
sprites:

- Do **not** alter `TerrainHill`'s LOS block, cover, or movement cost
  (server-derived in `SetTerrain` per ADR-0024).
- Do **not** introduce a new terrain type.

Tuning knob: `hillTreeFraction` (start 0.15). Revisit if hills start reading
as forest from the player's zoom level.

## Guards (required)

- Decoration trees must not change any tile's `BlockLOS`, cover, or
  `TerrainCosts` — verify by confirming the Hill branch in
  `buildTerrainObjects` only pushes render descriptors and touches no server
  data.
- Stronghold HP/garrison fields must degrade gracefully when 0/unset (older
  state messages, neutral strongholds pre-siege) — default `MaxHP` to
  `StrongholdHP(level)` client-side if absent.

## Verification

- **Stronghold:** run a match with a neutral stronghold; siege it with
  Cannon/Mission. Confirm the HP bar drains, the keep flips faction color on
  capture, HP restores, and garrison pips fill as CombatUnits enter (move a
  Squad onto it). Use the crash-restart e2e spec
  (`tests/e2e/zz-crash-restart.spec.js`) as the observation instrument for a
  manual eyeball in a 20v20 clash.
- **Dead-branch removal:** confirm no rendering change on any live map
  (ids 11–15 were already unreachable) — visual regression should be zero.
- **Trees:** visual inspection at play zoom — forest tiles read as shaped
  canopies, not flat rects; hill slopes show sparse decoration without hiding
  the hill tint or reading as forest.
- **Wire format:** `connection_test.mjs` covers structure parsing — extend it
  for the three new `StrongholdState` fields.
- No playtest-harness run required (no balance/cover/LOS change). If density
  tuning touches `hillTreeFraction` high enough to obscure terrain reads,
  revisit.

## Decision rationale

Resolved in the grill session against the existing domain model:

1. **"Move stronghold to building" was already decided (ADR-0023) and shipped
   server-side.** `placeStrongholds` (`server/pkg/tilemap/generate.go:467`)
   emits `StrongholdSpec` entities; terrain ids 11–15 are retired. The real
   gap was the client rendering flagged as a consequence in ADR-0023, not a
   new decision. → No new ADR; this plan executes the open consequence.

2. **Trees already exist** (Forest terrain + `main.js:1483` render). ADR-0024
   deliberately keeps trees as terrain, not entities. "Add trees" therefore
   means better sprites + cosmetic hill coverage — not a new type and not a
   balance change. Rejected: a distinct "Tree" scatter terrain (duplicates
   Rock/Brush, contradicts ADR-0024) and density tuning (a cover/LOS balance
   change belonging to the playtest harness, not "polish").

3. **HP + garrison occupancy are the indicators worth a wire change.** They
   express the two halves of the Stronghold-as-Building identity
   (destructible + garrisonable) that the current flat icon hides. A dedicated
   flip/capture-progress indicator was rejected as redundant — faction color
   change + HP-to-zero already convey the flip. Capacity stays client-derived
   to keep the wire lean.

4. **"Terrain polish" is capped at the above.** Edge-blend, palette tuning,
   underused terrain types, and elevation relief are each separate forks and
   were not the motivating itch; deferring prevents scope creep.

## Pointers

- `server/pkg/game/session.go:517` — `StrongholdStateIfChanged` (dedup).
- `server/pkg/game/session.go:529` — `StrongholdStates` (field emission).
- `server/pkg/game/session.go:592,597` — `StrongholdHP` / `StrongholdCapacity`
  spawn formulas to mirror.
- `server/pkg/component/stronghold.go` — garrison-count accessor (verify).
- `server/pkg/component/movement.go:17` — retired ids 11–15 reservation.
- `client/src/main.js:265` — `this.strongholds` field list.
- `client/src/main.js:1458,1483,1520,1659` — `buildTerrainObjects`,
  Forest branch, dead stronghold branch, `buildStrongholdDescriptors`.
- `client/src/app.js:317` — stronghold_state message wiring.
- `client/src/connection_test.mjs:133` — structure-parse test to extend.
- ADR-0023, ADR-0024, ADR-0015 (textured terrain shader).
