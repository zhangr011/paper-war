# Concealment and Elevation Advantage

**Status:** Accepted (2026-08-02)

Extends ADR-0024's terrain model (cover + LOS + movement cost) with two new
terrain-driven mechanics: **concealment** (hide in trees) and **elevation
advantage** (terrain height). Both reuse existing infrastructure — the
per-player fog grid, the spatial hash, and the discrete `Tile.Elevation`
bands (0/1/2, ADR-0024 / issue #49) — so there is no protocol/wire change and
no new entity type.

## Decision 1 — Concealment: a unit inside Forest/Brush is hidden from distant viewers

ADR-0024 made Forest/Wall/Rock block line-of-sight *past* them, and Brush is
"concealment only." But the fog raycast (`fog.hasLOS`) never checks the *end*
tile, so a unit standing **inside** a Forest/Brush tile was fully visible —
forests were just a 25% damage-reduction pad with no stealth.

Concealment closes that gap. A new predicate `component.Conceals(t)` returns
true for `TerrainForest` and `TerrainBrush` (the two soft-cover types). At
snapshot generation (`session.GenerateSnapshot`), an enemy on a concealment
tile is **hidden** from a player unless one of:

1. **Proximity detection** — one of the player's own units/commanders is within
   `fog.ConcealmentDetectionRadius` (3 tiles). Reuses the existing spatial
   hash `gs.Sh` that combat already queries — no new structure.
2. **Firing reveals** — the unit attacked within the last `fog.ConcealRevealTicks`
   (8 ticks ≈ 0.8 s @ 10 Hz), read from the existing `AttackComponent.LastAttack`.
   Attacking gives away an ambusher's position.

The concealed unit's **tile stays `FogVisible`** — the viewer still sees the
forest (terrain renders), just not the unit inside. This matches "hide in
trees": you see trees, not the ambushing squad.

Concealment is **server-side only and snapshot-scoped**. It does not touch the
fog grid, so AI targeting (`AISystem.scanEnemiesScored`, which reads `aiFog`
directly) and combat (`CombatSystem.findTarget`, spatial-hash based) are
unaffected — only what a *client* sees changes. `SnapshotGenerator` retains
the concealed unit's `prevStates` entry (only `ClearPrevStates` removes dead
IDs), so when concealment lifts the unit re-diffs normally — no respawn flash.

Rock/Wall are **not** concealment: they are hard LOS blockers handled by the
raycast, no unit stands on a Wall, and Rock is Heavy-impassable.

## Decision 2 — Elevation: high ground extends vision and attack range

`Tile.Elevation` (bands 0/1/2) was cosmetic — consumed only by the hill
shader and `DeriveElevation`. Elevation now has two combat-relevant effects:

1. **Vision** (`session.elevationVisionBonus`): each fog reveal source's radius
   is bumped by the viewer's tile elevation — peak (2) +3 tiles, slope (1) +1,
   low (0) unchanged. A commander (r12) on a peak sees r15; a combat unit (r6)
   on a peak sees r9. Peaks act as scout towers.
2. **Attack range** (`CombatSystem.Tick`): the attacker gains +1 tile of range
   per elevation level held **over the target** (peak vs low = +2), via a new
   `CombatSystem.ElevationFn` lookup wired in `session.go` alongside
   `TerrainFn`. Shooting uphill never shortens range — the defender already
   keeps the existing 15% hill cover bonus (`TerrainCoverBonus`), so stacking
   a range *penalty* on top would over-punish the attacker.

`ElevationFn` defaults to nil/0 in unit tests (flat maps), preserving all
existing combat-test behavior.

## Consequences

- New exported predicate: `component.Conceals(t TerrainType) bool`.
- New exported constants in `pkg/fog`: `ConcealmentDetectionRadius = 3`,
  `ConcealRevealTicks = 8`.
- New `CombatSystem.ElevationFn func(x,y int32) uint8` field, wired by the
  session over `gs.Map`.
- No wire-format change. Concealment is expressed as the absence of an enemy
  from a snapshot; elevation is server-internal. The client needs no change
  (concealed units simply vanish until detected or firing).
- Splash damage is unchanged (mirrors ADR-0024's splash-ignores-cover rule).

## Out of scope

- Client-side concealment indicator (a rustle/footprint cue) — deferred; the
  unit simply disappears for now.
- Elevation damage bonus (only vision + range were added).
- Destructible concealment (shooting a forest away) — still out of scope per
  ADR-0024.

## Note on balance tests

`pkg/game` mirror-balance tests (`TestPlaytestMatrix`, `TestStatsResetBetweenMatches`,
`TestFogEnemyAppearsAndDisappears`) are non-deterministic on `master` — they
fail on the same pinned seeds with or without this change, due to leaked
global/env state (`TestSeedFromEnvOrTime` subtests that use raw `os.Setenv`
without `t.Setenv`, plus `rand.Seed(time.Now())` in the matrix test). They are
not a sound regression gate for this feature; the deterministic coverage lives
in `pkg/component` (`TestConceals`), `pkg/combat`
(`TestCombatElevationRangeBonus`), and the existing `pkg/fog`/`pkg/tilemap`
suites.
