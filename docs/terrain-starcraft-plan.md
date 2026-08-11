# StarCraft-style Terrain System — Implementation Plan

Goal: elevate the existing decorative terrain into a gameplay system with
**elevation tiers + cliffs**, **ramps**, **high-ground vision/LOS**, **destructible
doodads**, and a **creep-equivalent** spreading faction terrain.

Grounded in the terrain survey (see navigation index at the bottom). Each phase
is independently shippable and lands behind a `go build` + `go test` gate.

---

## Shared design decisions (decided — do not re-litigate)

- **Elevation stays per-tile `Tile.Elevation uint8`** (`server/pkg/tilemap/tilemap.go:10`),
  tier model {0=low, 1=mid, 2=peak}. Generalize the rule so it applies to ALL
  terrain, not only Hill tiles (non-Hill tiles are simply elevation 0).
- **Cliff rule**: an adjacent step with `|Δelevation| ≥ 2` is impassable for
  ground units **unless** one of the two tiles is a `Ramp`. `|Δ| ≤ 1` is always
  walkable (subject to existing terrain cost). Implemented at the flowfield
  neighbor-expansion seam (`server/pkg/pathfinding/flowfield.go:51`).
- **New terrain id 18 = `TerrainRamp`**. Resize `MovementProfile.TerrainCosts`
  from `[18]` to `[20]` and update **every** indexed site (profiles.go, editor
  `PROFILE_COSTS`, map_validate, client state.js). Ramp is cheap walkable terrain
  (cost 1 Light / 1 Heavy) that permits crossing a 2-tier cliff.
- **Creep is an overlay, not a terrain type**: new field `Tile.CreepOwner uint8`
  (0 = none, 1/2 = faction). Effect: a unit whose faction equals `CreepOwner`
  gets a movement discount (cost × 0.7) on that tile; enemy creep is neutral.
  This avoids conflating creep with the terrain-type array.
- **Live terrain changes ride the existing reserved `EventTerrainChange` (type 2)**
  (`server/pkg/network/snapshot.go:41`) — already defined + client-handled
  (`client/src/state.js:806`, `client/src/particles.js:158`). The server just
  needs to start emitting it from `TerrainSystem.Tick`.
- Read terrain LOS via the `component.BlocksLOS(tt)` function, NOT the retired
  `Tile.BlockLOS` field (`tilemap.go:91`).

---

## Phase 1 — Elevation tiers, cliffs, ramps (pathfinding + data)

The foundation every other phase depends on.

1. Add `TerrainRamp = 18` constant in `server/pkg/component/movement.go`.
2. Resize `MovementProfile.TerrainCosts` to `[20]uint8`; add Ramp cost entries
   in `StandardMovementProfile`s (`server/pkg/component/profiles.go:6`).
3. `server/pkg/pathfinding/flowfield.go:27` (`Compute`): when expanding a
   neighbor, compute elevation delta from `gm.TileAt`. Skip the neighbor if
   `|Δ| ≥ 2` AND neither current nor neighbor tile is `TerrainRamp`. Add a
   helper `gm.EdgeWalkable(x1,y1,x2,y2,profile)` so the rule lives in one place
   (`server/pkg/tilemap/tilemap.go` near `CostAt` :95).
4. `server/pkg/tilemap/elevation.go:16` (`DeriveElevation`): leave as-is for
   Hills; document that cliffs emerge from existing elevation data.
5. Author ramps in one clash map (`ClashHills`, `clash_maps.go:316`) so the
   two spawns remain connected after cliffs block direct routes — verify with
   the existing connectivity validator (`map_validate.go`).
6. Client: add `TerrainRamp` (18) to `TERRAIN_COLORS` (`client/src/main.js:191`)
   + editor `TERRAIN_NAMES`/palette (`client/editor/map_editor.js:20,309`) +
   fix stale `PROFILE_COSTS` to 20 entries (:94).
7. Gate: `cd server && go build ./... && go test ./pkg/pathfinding/... ./pkg/tilemap/...`
   + add a pathfinding test asserting a 2-tier cliff is impassable without a
   ramp and passable with one.

## Phase 2 — High-ground vision/LOS + combat advantage

1. `server/pkg/fog/fog.go:83` (`hasLOS`): make height-aware. A viewer on
   elevation `Ev` is blocked only by intermediate tiles whose elevation
   `> Ev` (high ground sees over low). Pass viewer elevation into `RevealRadius`
   → `hasLOS`. Keep start/end tile behavior (blocker tile itself visible).
2. `server/pkg/game/session.go:383` (`updateFog`): thread viewer elevation
   (from `TileAt(src).Elevation`) into each vision source.
3. Combat advantage: in the combat resolution path (`server/pkg/combat/*`),
   add a high-ground bonus when `attackerTile.Elevation > targetTile.Elevation`
   — e.g., +1 tile range OR +X% hit chance. Pick **+1 range** (simplest,
   matches SC feel) implemented where range is checked.
4. Gate: `go test ./pkg/fog/... ./pkg/combat/...` + a fog test asserting a
   peak-dwelling viewer sees over a low Forest that blinds a low-ground viewer.

## Phase 3 — Destructible doodads wired to combat + wire event

1. Give destructible doodads HP: in `server/pkg/tilemap/generate.go` scatter
   sites + `clash_maps.go`, set `MaxHealth` on `TerrainRock` (e.g. 300),
   `TerrainForest` (200), `TerrainWall` (400). Bridge already 200.
2. Extend `getDestroyedTerrain` (`server/pkg/terrain/dynamic.go:78`):
   Rock→Plain, Forest→Plain, Wall→Plain, Bridge→Deep.
3. Wire combat→terrain: when a projectile/AoE impacts a tile with a
   destructible doodad, call `terrainSys.ProcessDestruction(x,y,dmg)`. Find the
   impact site in `server/pkg/combat/*` (projectile resolution / AoE).
4. Emit `EventTerrainChange` from `TerrainSystem.Tick` (`dynamic.go:35`) for
   each applied event, appended to the snapshot event stream
   (`server/pkg/network/snapshot.go`). Client already renders it.
5. Gate: `go test ./pkg/terrain/...` + a test firing damage at a Rock and
   asserting it becomes Plain and an `EventTerrainChange` is produced.

## Phase 4 — Creep-equivalent spreading faction terrain

1. Add `Tile.CreepOwner uint8` (`tilemap.go:5`).
2. New ECS system `CreepSystem` (`server/pkg/creep/creep.go`): every N ticks,
   spread creep from owned sources (faction commanders / strongholds) to
   orthogonally-adjacent walkable tiles; decay enemy creep when contested.
   Register in `session.go` near `:212`.
3. Movement: in `CostAt`/flowfield, apply friendly-creep discount
   (cost × 0.7 when `tile.CreepOwner == unitFaction`).
4. Wire: ship the creep grid to clients. Add a compact binary creep-overlay
   message (raw `w*h` bytes, 0/1/2 per tile) sent at 2Hz from
   `server/cmd/server/main.go` snapshot pump. Client stores `creepData`
   (`client/src/main.js`) and tints creep tiles (faction color overlay in
   `buildTerrainTiles` :1404 / a shader branch in `gl.js`).
5. Gate: `go test ./pkg/creep/...` (spread-from-source test) + a manual clash
   confirming visual + speed effect.

---

## Acceptance (whole feature)

- `cd server && go build ./... && go test ./...` green.
- A clash on `ClashHills`: armies must route via ramps (cliffs block),
  high-ground units see + shoot further, rocks/trees are destroyable, and the
  owning faction's creep visibly spreads and speeds its units.
- No existing test regresses; new tests added per phase.

## Navigation index (load-bearing file:line)
- tilemap: `server/pkg/tilemap/tilemap.go:5,41,76,83,95`, `elevation.go:16`, `generate.go:72,96,453,700`, `clash_maps.go:316,380,409`, `connectivity.go:150`
- components: `server/pkg/component/movement.go:5,29,45,54`, `profiles.go:6`
- terrain sys: `server/pkg/terrain/dynamic.go:15,35,66,78`
- fog: `server/pkg/fog/fog.go:27,61,83,146`; session `server/pkg/game/session.go:383,471,2037`
- pathfinding: `server/pkg/pathfinding/flowfield.go:27,51`, `cache.go:20,69`
- wire: `server/pkg/network/snapshot.go:41`; main `server/cmd/server/main.go:123,308,325,459,505`
- combat: `server/pkg/combat/*`
- client: `client/src/main.js:191,390,1404`, `client/src/gl.js:62,73`, `client/src/state.js:806`, `client/src/connection.js:30,55`
- editor: `client/editor/map_editor.js:20,94,309,446`
