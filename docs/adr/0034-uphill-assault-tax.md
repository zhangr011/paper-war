# 0034 — Uphill Assault Tax and Ramp Semantics

**Status:** Accepted (2026-08-16)

## Context

The 2026-08-16 "the team on the hill lost the clash" investigation established
that high ground's combat benefits worked (post acquisition-gate fix, the hill
side wins 79–98% of production-shaped `hills` clashes) but nothing taxed the
*approach*: an enemy could walk onto the holder's elevation at normal hill-walk
speed, and once at equal elevation every advantage evaporated. Movement cost
is destination-terrain only (`CostAtFor`) — no uphill/downhill distinction
existed anywhere in the model.

Two candidate mechanisms were tested on the `hills` clash map (50-run
production-shaped arms each):

- **Map-side chokepoints** (re-authored lip to Δ2 cliff + 2-wide Ramp passes,
  the `hills_validation` pattern): hill side won **0/50 in both arms**. In
  clash mode both armies are force-marched through the same choke, so the
  hill army exits its own gate single-file into a wall of fire. A choke
  favors a defender; the mode has none. Refuted structurally, not
  parametrically — widening the passes changed nothing.
- **Sim-side climb tax**: taxes the approach without walling anyone in.

A second leak surfaced in the same investigation: Ramp tiles carry an authored
elevation (typically the top band of the ridge they serve), so a climber
standing mid-ramp gained the full high-ground +1 range — the assault was
rewarded mid-crossing.

## Decision

1. **Uphill Cost** (`EdgeWalkableFor`): stepping onto a higher Elevation band
   costs **+1 per band climbed** (`UphillStepCost`), on top of destination
   terrain cost. Downhill and level steps are free. Cost only — the surcharge
   never makes an edge impassable, so no map can newly disconnect.
2. **Ramp exemption**: steps with a Ramp tile on either end pay no Uphill
   Cost. The Ramp is the fast, channeled route across a cliff; open slopes
   are the slow route. Maps thus have two distinct elevation-assault tools.
3. **Ramp grants no High Ground benefits**: a unit standing on Ramp terrain
   gets no range bonus and no acquisition extension, regardless of the
   Ramp's authored elevation (fire gate and acquisition gate in
   `CombatSystem.Tick`).
4. **Ramp authoring convention**: author a Ramp at the LOWER band of the
   ridge it serves (e.g. e1 serving an e2 ridge), so a ridge defender
   outranges a climber for the entire crossing. `hills_validation` follows
   this convention (8 ramp cells at e1); its guard test pins it.

## Consequences

- Light Infantry pays 3→4 per uphill Hill step, Heavy 4→5 (K=+1). Retune
  `UphillStepCost` alongside `TerrainCosts` if assault pacing shifts.
- Measured effect on `hills` production clashes: 82%/96% hill wins (from
  79%/98% post-acquisition-fix) — a small nudge where the holder already
  won; the systemic value is on maps where the climb is the whole approach.
- Edge costs feed flowfields and validators identically; connectivity is
  unchanged (surcharge never blocks).
- Glossary: **High Ground** (fire platform), **Ramp** (transition strip),
  **Uphill Cost** (approach tax) — see `CONTEXT.md`.

## Out of scope

- Climb vulnerability (no-firing / bonus-damage mid-climb states) — a combat
  change with a much larger blast radius; revisit only if the tax proves
  insufficient.
- Elevation damage bonus — still out of scope per ADR-0029.
- Re-authoring further clash maps — `hills` keeps its open Δ1 mouth by
  design (the chokepoint variant measured strictly worse).
