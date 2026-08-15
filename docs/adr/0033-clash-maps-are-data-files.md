# 0033 — Clash Maps Are Data Files, Not Go Source

Date: 2026-08-15

## Context

Clash maps lived as hand-authored Go source: seven `ClashXxx()` functions in
`server/pkg/tilemap/clash_maps.go`, each painting tiles with `SetTerrain`
calls, plus a `LoadClashMap` switch-case to register each name. The map editor
(`client/editor/map.html`) could paint terrain but its only output was
**generated Go source the user had to copy-paste into `clash_maps.go` and then
register by hand** — a manual two-step that recreated every drift problem
ADR-0022 killed for snapshots ("no hand-copied file to drift").

Two aggravating facts made the status quo worse than inconvenient:

1. `LoadClashMap` unconditionally ran `DeriveElevation(m)`, which overwrites
   `Elevation` on every Hill tile from topology — so even the editor's
   elevation export was being silently re-derived at load time.
2. The Go-source path could not be written to at runtime without regenerating
   code (rejected: the running binary never re-reads `clash_maps.go`, so a
   save would silently not take effect until a manual rebuild+restart — the
   exact manual step we were removing).

## Decision

Clash maps are JSON data files at `server/data/clash_maps/<name>.json`, in the
same wire shape the editor already spoke (`{w,h,terrain,elevation}`, row-major
`[]int` arrays — the `clashMapSnapshot` format of `GET /editor/clash-maps`).

- **Loading**: `LoadClashMap` reads the JSON directory first
  (`clash_json.go`); a saved map is constructed once per match start, so a
  file read is negligible. `"random"` draws from the six rotation maps' files.
- **Saving**: the editor POSTs to `/editor/clash-maps/save`; the handler
  validates and writes atomically (temp + rename). A saved map is loadable by
  the next `start_clash` with no rebuild or restart.
- **Name slugs**: `[a-z0-9_]{1,32}` — the path-traversal guard, enforced on
  both load and save.
- **Elevation is authored, not derived**: the JSON path never calls
  `DeriveElevation`. The legacy path needed it because Go-source maps authored
  Hill tiles without elevation; JSON authors the elevation grid directly.
- **Destructible HP** (Wall 400 / Rock 300) is not in the wire format — the
  loader restores it per terrain type. Accepted normalization: the legacy
  stronghold map carried stray HP on one Forest tile (wall placed, forest
  painted over), making that tile accidentally destructible.
- **Migration**: the seven Go maps were exported through `LoadClashMap` (i.e.
  post-`DeriveElevation` — exactly the state live play saw) by a one-shot
  `go run ./cmd/tools/export-clash-maps`; `TestClashJSONMatchesLegacyGo` gates
  the equivalence; the Go map bodies are deleted after it passes.

Supersedes the export-Go-source consequence of ADR-0022 (which stands
otherwise: the editor authors terrain/elevation only; spawns and objective are
forced at runtime).

## Consequences

- The editor's save loop is closed: paint → Save → `start_clash` on the saved
  name, all without touching Go source or restarting the server.
- `GET /editor/clash-maps` lists the data directory, so new maps appear in the
  editor's Load dropdown automatically.
- Authored elevation survives loading (fixes the `DeriveElevation` clobber).
- The seven map definitions move from ~500 lines of Go to 7 reviewable JSON
  files, diffable in PRs like any other data.
- No auth on the save endpoint — same dev-tool posture as `/editor/ai`
  (`main.go`). The slug regex bounds what files can be touched.
- `DeriveElevation` loses its last clash call site; it remains for procedural
  `GenerateMap`'s topology grading if ever needed, but nothing in the clash
  path uses it.
