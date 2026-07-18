# Environment Objects: Line-of-Sight and the Terrain Model

**Status:** Accepted (2026-07-18)

Resolves the two open design forks from issue #55's triage: how line-of-sight
works (Phase 2), and whether environmental objects are terrain types or map
entities (Phase 3).

## Decision 1 — LOS: per-entity viewers, per-tile Bresenham raycast

Fog keeps its existing **per-entity viewer** model (each Commander r12 and
CombatUnit r6 is a `RevealRadius` source writing into a per-player `FogGrid`)
and adds a **per-tile line-of-sight gate** inside `RevealRadius`: a candidate
tile is revealed only if a Bresenham line from the viewer reaches it without
crossing a `BlockLOS` tile. Start and end tiles are never checked, so a viewer
sees out of its own tile and sees a blocker itself — just not past it.

Why not per-tile viewers (a global visibility grid): the architecture already
fans vision out per-entity per-player, and `updateFog` runs every tick; a
per-tile model would force a restructuring of fog + AI visibility queries for
no gain. The raycast is cheap — 6.9 µs per commander-radius reveal on a 30×48
map (`BenchmarkRevealRadiusLOS`), ~0.7 ms/tick for ~100 viewers at 10 Hz, far
inside the 100 ms budget.

`Tile.BlockLOS` is now **derived from terrain in `SetTerrain`**
(`component.BlocksLOS`), retiring its dead-field status. Every placement —
generator, clash maps, editor-pasted maps — stays correct without each site
re-deriving it.

## Decision 2 — environment objects are terrain types, not entities

Trees, rocks, and brush stay **terrain types**, not map entities. They need
only cover + LOS + a movement cost — no ownership, no HP, no garrison — and
destructible environment is explicitly out of scope (#55). Phases 1 (cover)
and 2 (LOS) are already terrain-derived, so terrain is the consistent and
cheapest model.

This **deliberately contrasts with Stronghold** (ADR-0023), which moves to a
Building entity because it needs ownership, HP, and garrison. The split:

- **Static, non-owned environment → terrain type** (Forest, Hill, Rock, Brush,
  Wall). Effects = cover + LOS + movement cost, all derived from the type.
- **Owned / interactive structure → entity** (Stronghold, Watchtower, Turret,
  Barricade). Effects require HP, faction, and runtime state.

New terrain types added (Phase 3): **Rock** (cover 40%, blocks LOS, passable
but slow for both profiles so it never cuts Heavy routes) and **Brush**
(cover 10%, no LOS block — concealment only). `MovementProfile.TerrainCosts`
widened from `[16]` to `[18]` to fit them.

## Consequences

- Two new terrain ids (16 Rock, 17 Brush); `TerrainCosts` is `[18]uint8`. Any
  future terrain type reuses the same derive-from-terrain pattern.
- The Clash Map editor (#53) palette + `client/src/main.js` `TERRAIN_COLORS`
  carry the two new entries; `applyScatter` in the procedural generator places
  them (rock on hills, brush on plains, sparse).
- Splash damage still ignores cover (mirrors the pre-existing stronghold
  behavior); a follow-up can apply terrain defense at the pending-damage loop
  if splash should respect cover too.

See issue #55.
