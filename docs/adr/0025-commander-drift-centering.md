# 0025 — Drift-Based Commander Centering

Date: 2026-07-31

## Context

A Squad whose Commander ends up behind its CombatUnits tends to lose the
engagement. The movement model has no force that keeps the Commander centered
within its Squad: CombatUnits get a flow-field force *plus* an attraction force
toward `commanderPos + formationOffset` (`server/pkg/movement/movement.go:132-133,
154-165`), while the Commander gets *only* the flow-field force and separation
(`movement.go:142-144`). When CombatUnits surge forward in combat — each unit
flows/pursues enemies at its own speed, and `combat.go:192-197` sets per-unit
pursue targets — a slow Commander falls behind the squad centroid and nothing
recenters it.

(Note: formation offsets are dead in the live server — `CalcOffsets` has no
non-test callers and `FormationRoleComponent.OffsetX/Y` are never written — so
every CombatUnit attracts to the Commander's exact position; see `CONTEXT.md` *
Commander* / *Flagged ambiguities*. The "surge" is purely the flow-field force.)

We considered four ways to keep the Commander at center: (a) add an attraction
force on the Commander toward the squad centroid, (b) hard-clamp the Commander
to the centroid each tick, (c) speed-sync CombatUnits to the Commander, and (d)
suppress the CombatUnit surge so they hold around the Commander. (b) was rejected
because it teleports and conflicts with the server-side attack-freeze work
(`combat.go:281-289`); (c) changes movement feel for every unit and puts the
Commander at the front rather than center; (a) was viable but adds a new force
on the Commander. We chose (d).

## Decision

Suppress the CombatUnit surge via **drift-based centering**. Each tick,
`CommanderSystem` (Priority 50, before Movement at 60) measures the Commander's
distance from its Squad centroid (mean position of alive non-Commander squad
members) and sets a per-Squad `Suppressing` flag with hysteresis:

- Suppress when `distSq > FromFloat(0.5)²` (Commander > 0.5 tile from centroid).
- Release when `distSq < FromFloat(0.2)²` (back within 0.2 tile).

While suppressing, `MovementSystem` zeroes the flow-field force (`flowFX/flowFY`)
for non-Commander units of that Squad (`movement.go:132-133`), so the existing
attraction-to-Commander force dominates and they collapse back onto the
Commander — recentering it. The Commander keeps its own flow force, so it still
leads, and the Squad advances as a block anchored to it.

Applied symmetrically to **all Squads (player + AI, both Factions)**.

### Why such tight thresholds

With `combatUnitCrossMapSeconds=240` and a 48-tile long axis, a unit moves
`48/240 = 0.2 tiles/sec = 0.02 tiles/tick`. The 0.3-tile hysteresis band is
therefore ~15 ticks wide — far wider than per-tick motion, so the loop cannot
oscillate. The tight `D_high=0.5` deliberately keeps the Commander within half a
tile of the centroid at all times, producing very strong cohesion: in a dynamic
fight suppression trips almost immediately and the Squad stays glued to the
Commander.

## Consequences

- The Commander can no longer trail its Squad — it is held at the squad centroid
  by construction, directly addressing the "rear-Commander → lose" pattern.
- **Stall risk:** while suppressing, non-Commanders cannot close on enemies
  under their own flow. The Squad only advances into engagement range as fast as
  the Commander does. A passive, slow, or rear-hanging Commander stalls the
  whole Squad. The fix assumes the Commander leads toward the enemy — true for
  the AI (which drives its Commander at objectives) and for ordered players, but
  a Commander given no attack/move order will not engage.
- Formation offsets remain dead; this centering is independent of the
  `FormationRoleComponent` path. If formation offsets are ever wired up
  (`CmdChangeFormation`), suppression will pull units to the Commander, not to
  their formation slot — revisit then.
- Per-tick cost: one O(N) pass over the boid pool to accumulate per-squad
  centroid sums, plus the existing commander pass. The aura spatial query
  (3-tile radius) is *not* reused — it would miss surged units beyond aura range.
- Hysteresis thresholds are tuning knobs. If cross-map pacing changes
  (`combatUnitCrossMapSeconds`) or maps grow, re-check that per-tick motion stays
  below the 0.3-tile band.
