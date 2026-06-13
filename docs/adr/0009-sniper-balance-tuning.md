# 0009 — Sniper Balance Tuning

Date: 2026-06-13

## Context

Issue #5 reported that Sniper units win vs Light Infantry with 100% HP remaining,
making them completely dominant. The original stats gave Snipers excessive range
advantage combined with a 150% damage multiplier vs Light armor, allowing them to
kill all approaching infantry before taking any damage.

Original stats:
- Sniper: HP=40, Dmg=45, Range=10, CD=8, Sniper vs Light=150%
- LightInfantry: HP=80, Dmg=15, Range=5, CD=3

Simulation (plains clash map, 18 units total) showed:
- Snipers killed all LIs before any LI entered firing range
- Sniper team retained 100% HP (zero losses)

## Decision

Rebalance Sniper as a true glass cannon — fragile, slow-firing, moderate per-shot
damage, with range advantage but not overwhelming alpha strike capability.

Changes:

| Stat          | Before | After |
|---------------|--------|-------|
| Sniper HP     | 40     | 30    |
| Sniper Damage | 45     | 20    |
| Sniper Range  | 10     | 8     |
| Sniper CD     | 8      | 12    |
| Sniper vs Lt  | 150%   | 100%  |
| LI HP         | 80     | 100   |

Damage matrix Sniper row: `{100, 25, 0}` (was `{150, 25, 0}`).

Simulation result: Snipers win with ~69% HP remaining. This is within acceptable
bounds — pure Sniper vs pure LI is an ideal matchup for Snipers. In actual games,
mixed compositions and terrain will produce closer outcomes.

## Consequences

- Sniper is no longer an anti-infantry hard counter (100% vs Light, neutral)
- Sniper retains role as long-range harasser with low DPS
- Anti-Armor Infantry (Missile) remains the anti-Heavy specialist (150% vs Heavy)
- Heavy Infantry (Cannon) remains the anti-structure unit (25% vs Building)
- Light Infantry HP buff (80→100) improves survivability across all matchups
