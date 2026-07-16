# Map Editor Authors Clash Maps, Not Match Maps

**Status:** Accepted (2026-07-17)

The codebase has two map subsystems that share the `GameMap` struct but differ in every other respect, and "map editor" could target either. We decided the editor authors **Clash Maps** only.

- **Clash Map** — hand-authored in `server/pkg/tilemap/clash_maps.go`, 32×32 square, selected by name via `LoadClashMap`. Used only by the `start_clash` spectator/balance harness, which overrides Spawns (forced to map center, 8 tiles apart, `main.go:377-389`) and Objective (forced to Elimination, `main.go:348`) at runtime. Only the terrain is live.
- **Match Map** — procedural via `GenerateMap`, 30×48 portrait (`DefaultMapWidth/Height`, issue #45), top/bottom spawns, full Objective (Elimination/Capture/Survival). Used by solo and PvP queue. There is no hand-authored match-map code path today.

The editor exports a new `ClashXxx()` function (Go source) to paste into `clash_maps.go` plus a `LoadClashMap` case — mirroring the `#50` animation editor and the units editor. It does **not** introduce a map-data format or a hand-authored match-map path.

## Why clash maps first

1. Acute pain is hand-editing `setSym`/coordinate tables in `clash_maps.go` — the clash surface is what the ongoing balance work (MotorGun pass, `TestPlaytestMatrixRealistic`) actively exercises.
2. It is the third paste-back dev editor in the established family; bounded, no runtime integration, ships in days.
3. A match-map editor is a superset that benefits from this existing: the terrain-paint + symmetry + preview surface is reusable once a hand-authored match-map path is worth building (its own issue/ADR, since it invents a runtime path and leaks into the matchmaker).

## Consequences

- Because clash mode overrides Spawns and Objective, an MVP editor that modeled those fields would present inert controls. The editor's contract is therefore **terrain (+ elevation) only** until the override is loosened (a flagged follow-up: let a clash map's own Spawns/Objective win when present). No control is shown that does not affect output.
- The two-subsystem split is now explicit. Future work that assumes "the map" is one thing will hit this fork; this ADR is the pointer.
