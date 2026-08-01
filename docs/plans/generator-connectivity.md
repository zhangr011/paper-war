# Plan — Generator Connectivity Guarantee

The solo match stalemates ~93% of the time (only seeds 8 and 21 of 1–30 end
cleanly). Root cause, fully traced this session:

1. The procedural map's river/lake (`TerrainDeep`) can bisect the map into
   disconnected regions for a movement profile.
2. `TerrainDeep` is impassable (cost 0) for both Light and Heavy profiles
   (`server/pkg/component/profiles.go`).
3. Bridges (`TerrainBridge`) are the only Deep→passable crossing, and
   `placeBridges` places ~1–2 of them at the *narrowest X-crossings* — which
   does not reliably reconnect a meandering river that spans many X-columns.
4. When a unit's target is in a disconnected region, the flow field
   (`server/pkg/pathfinding/flowfield.go:69`) leaves its tile's direction at
   `{0,0}` (cost = MAX). A commander gets movement force **only** from the
   flow field (`server/pkg/movement/movement.go:127-137`, commanders get no
   attraction force), so it freezes forever. The AI squad targeting a
   survivor across the river does the same. Neither side can reach the other
   → elimination never completes → stalemate.

PR #61 (full-spanning bridges) was a necessary correctness fix but is
insufficient: seed 1 still stalemates because the river meanders across many
X-columns and the 1–2 narrow-X bridges don't reconnect every bisect.

## Goal

Guarantee that every generated map is **connected for every movement
profile** — any passable tile reachable from any other passable tile using
that profile's passability rules. With that guarantee, no unit's target is
ever unreachable, the flow field is never zero at a reachable tile, and the
stalemate class is eliminated.

## Scope

1. A per-profile connectivity check over the generated map.
2. A repair pass that adds crossings (Shallow fords / extra bridges) until
   connected, for each profile that needs it.
3. A fallback regenerate-and-retry if repair can't connect within a bound.
4. Tests asserting full connectivity across N seeds for both profiles.
5. Re-pin `TestSoloMatchRunsToCompletion` off the lucky seed-8 crutch once
   stalemates are actually gone.

## Out of scope

- Changing `TerrainDeep` passability (Deep stays impassable — it's the whole
  point of water as a barrier; bridges/fords are the crossings).
- Movement-system changes (a zero flow field for a *genuinely* unreachable
  target is correct behavior once connectivity is guaranteed — the target
  will always be reachable).
- AI "hunt/search" behavior (separate; only matters once a target is
  reachable, which this plan ensures).
- Clash maps (hand-authored; separate connectivity story if needed).

## Design

### Profile passability

A tile is passable for a profile when `gm.CostAt(x,y,profile) > 0` (matches
the flow-field Dijkstra gate at `flowfield.go:52`). Light crosses
`TerrainShallow` (cost 2); Heavy does not (cost 0). So **Light and Heavy have
different reachable sets** — connectivity must hold for BOTH (and any profile
the game fields). Use `component.StandardMovementProfiles()` and check each.

### Connectivity check

Flood-fill (4-neighbor BFS) over passable tiles from an arbitrary passable
seed tile. If the visited count equals the total passable-tile count, the map
is single-component for that profile. O(W·H) per profile — cheap (32×48).

`func (m *GameMap) ConnectedFor(profile *MovementProfile) bool` — add to
`server/pkg/tilemap/` (new file `connectivity.go`) or as a method on GameMap.

### Repair pass

When `ConnectedFor(profile)` is false, find the disconnected passable
components and connect them by converting Deep tiles along their shared
boundary into crossings:

- **For Light**: convert a 1-tile-wide `TerrainShallow` band at the boundary
  (Light crosses Shallow). Cheapest, most natural ford.
- **For Heavy**: convert a `TerrainBridge` at the boundary (Heavy crosses
  Bridge). Place via the existing bridge tile type.

Concretely: BFS to label components; for each pair of adjacent components
separated by Deep water, convert the minimum Deep tiles needed to join them
(a short ford/bridge perpendicular to the river). Recompute connectivity;
repeat until connected or an iteration cap (e.g., 8) is hit.

This guarantees a crossing exists wherever water separates components —
unlike the X-narrow heuristic, which only bridges where the river happens to
be X-narrow.

### Retry fallback

If repair can't connect a map within the cap (e.g., a pathological lake),
regenerate with a new seed. `GenerateMap` should loop: generate → check both
profiles → repair → if still disconnected, retry with seed+1, up to a small
max (e.g., 5). Log a warning if the cap is hit (shouldn't happen post-repair).

## Verification — `server/pkg/tilemap/connectivity_test.go`

- `TestConnectivity_AllSeeds`: for seeds 1–30 (or a representative set),
  `GenerateMap(...)` is connected for BOTH Light and Heavy profiles. This is
  the headline regression test — would have caught the stalemate root cause.
- `TestConnectivity_Repair`: construct a hand-built map with a known bisecting
  river and assert the repair pass reconnects it for both profiles.
- `TestConnectivity_RetryCap`: (optional) a forced-unconnectable fixture hits
  the retry cap without panicking.

## Re-pin the stalemate test

After connectivity holds across seeds:
- Sweep seeds 1–30 again with `TestSoloMatchRunsToCompletion`-style runs.
  Most/all should now end. Re-pin the test to a stable seed (or, ideally,
  remove the seed-crutch and assert across several seeds).
- If some seeds still stalemate for a *different* reason (e.g., the
  survivor-hunt gap, not connectivity), report it — don't mask. Connectivity
  eliminates the dominant cause; any residual is a separate AI/mop-up fix.

## Verify before reporting

- `cd server && go build ./...`
- `cd server && go test ./pkg/tilemap/ -v` — new connectivity tests pass,
  existing suite (elevation, bridge, generator contracts) stays green.
- `cd server && go test ./pkg/game/ -run TestSoloMatchRunsToCompletion` —
  green on the pinned seed; ideally try `-count=1` across a few seeds to
  confirm the stalemate rate dropped.
- `cd server && go test ./...`
- `cd server && go vet ./pkg/tilemap/ ./pkg/game/`

## Report back

Concise: files added/changed with line ranges, the connectivity-check +
repair algorithm chosen, before/after stalemate rate over a seed sweep
(e.g., seeds 1–30: was 28/30 stalemate → now ?/30), and whether
`TestSoloMatchRunsToCompletion` could be un-crutched. If you hit a seed that
stalemates post-fix for a non-connectivity reason, capture the seed + symptom
for the next investigation. Branch `generator-connectivity` off master; one
commit; don't push (I'll push/merge after review).

## Pointers

- `server/pkg/tilemap/generate.go` — `GenerateMap` (retry loop site),
  `traceRiver`/`fillLake` (water placement), `placeBridges` (X-narrow bridges).
- `server/pkg/tilemap/tilemap.go` — `GameMap`, `TileAt`, `CostAt`, `Width/Height`.
- `server/pkg/component/profiles.go` — `StandardMovementProfiles()`,
  `ArmorTypeToProfileID`; `movement.go` — `TerrainCosts`, `TerrainDeep=3`,
  `TerrainShallow=2`, `TerrainBridge=7`.
- `server/pkg/pathfinding/flowfield.go:52,69` — the passability gate +
  unreachable→zero-direction behavior this plan prevents.
- `server/pkg/movement/movement.go:127-137` — why a zero flow freezes commanders.
- `server/pkg/game/solo_match_integration_test.go` — the stalemate test to
  re-pin. PR #61 (full-span bridges) is the partial fix this supersedes in scope.
