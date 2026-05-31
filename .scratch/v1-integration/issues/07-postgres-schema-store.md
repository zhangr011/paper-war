Status: done

## Parent

`.scratch/v1-integration/PRD.md`

## What to build

Create a PostgreSQL schema and implement the real PostgresStore that fulfills the Store interface defined in `pkg/persist/store.go`. Currently only MockStore exists; the PostgresStore is a stub behind `//go:build !nopgx`.

Changes:
1. Create `pkg/persist/schema.sql` with tables: `players` (id, token, created_at) and `commanders` (id, player_id, weapon, armor, unit_type, leading_skill, kill_points, level, formation JSONB, combat_units JSONB, created_at).
2. Implement `pkg/persist/postgres.go` (behind `//go:build !nopgx`) with real pgx queries: FindOrCreatePlayer, LoadRoster, SaveCommander, DeleteCommander, CreateStarterRoster.
3. In `cmd/server/main.go`: accept `DATABASE_URL` env var. If set, create real PostgresStore + run migration. If not set, fall back to MockStore (in-memory).
4. Keep existing tests passing with `-tags nopgx` (MockStore only).

## Acceptance criteria

- [ ] schema.sql creates players and commanders tables
- [ ] PostgresStore implements all Store interface methods with real SQL
- [ ] main.go connects to PostgreSQL when DATABASE_URL is set
- [ ] Falls back to MockStore when no DATABASE_URL (dev mode)
- [ ] Existing tests still pass with `-tags nopgx`
- [ ] Test: FindOrCreatePlayer creates new player with starter roster
- [ ] Test: SaveCommander persists formation and combat_units JSONB

## Blocked by

- Issue 05 (Roster deploy — Store interface finalized after deploy logic)
