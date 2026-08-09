# 0030 — Hard Collision for Friendly-Unit Spacing

Date: 2026-08-07

## Context

CombatUnits stack to a single point because every non-Commander unit attracts to
the Commander's *exact* position (`server/pkg/movement/movement.go:155-159`) —
formation offsets were deleted in `0fa5710` and never replaced. Two cures exist:
(a) give each unit a distinct attraction target (restore ring offsets), or (b)
keep one attraction target and add positional collision.

Option (a) was tried and removed. The issue-#71 `gs.Tick` scatter (~0.22
tile/tick) smeared units off any slot finer than ~0.3 tile (`7971bea`: "crisper
resolution needs the gs.Tick scatter fixed, not more spacing"), and rather than
fix #71 the formation system was deleted outright (`0fa5710`). Option (a) is
*open-loop* — scatter drift is never corrected — so it looks loose against an
unfixed #71. Option (b) is *closed-loop*: an overlap smeared by the scatter this
tick is pushed back out next tick, so the packed cluster radius stays tight
regardless of the noise.

## Decision

Option (b): **hard positional collision among friendly units** (same faction,
including cross-squad), resolved by push-out after force integration. This is a
positional correction, *not* a repulsion force — a force would re-import the
attraction/repulsion oscillation excised in `ccc6aeb`.

- Broad phase: spatial-hash query (the existing `spatial.Hash`).
- Narrow phase: distance-squared circle test against a per-type radius.
- Resolution: one-pass positional push-out in entity-ID order.
- New `CollisionSystem` at priority 65 (after Movement 60, before Combat 80);
  it rebuilds the hash from post-move positions, then resolves.
- Per-type collision radius lives in `CombatUnitStats`
  (`server/pkg/component/unit_type.go:36`), replicated to a component for the hot
  path.
- **Garrisoned units are excluded** (they stack at their Stronghold's position by
  design — `server/pkg/component/boid.go:23`).
- **Attack-frozen units (#52) are immovable obstacles** — others push off them,
  they themselves never move, preserving the firing line without breaking the
  server-side freeze that prevents client teleport.
- **Enemy units do not collide** — combat is ranged (ADR-0003); physical blocking
  was not the motivating symptom and would change fight feel.

## Consequences

- Stacking is fixed for friendly units; two friendly Squads no longer merge into
  one point.
- #71 remains unfixed. Expect residual jitter at contact boundaries, but no
  stacking. Spacing tighter than the per-type radii imply is not achievable until
  #71 is addressed — that is a separate ticket, deliberately out of scope here.
- Supersedes the "cohesion-only" model introduced by `ccc6aeb` and restores
  ADR-0003's spacing intent in *hard* (positional, not force) form, scoped to
  friendly pairs. ADR-0003's description of soft "separation" remains retired —
  see the **Collision** term in `CONTEXT.md`.
- One extra spatial-hash rebuild per tick. Cost is bounded by the friendly-unit
  count (≪ total), so it is trivial vs. the existing per-tick work.
