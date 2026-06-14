# 0008 — PostgreSQL Roster Persistence

Date: 2026-06-13

## Context

The game persists player rosters (commanders, combat units, formation templates, gold) between sessions. The initial implementation used an in-memory `MockStore` that loses all data on server restart.

For v1 production, rosters must survive restarts and be queryable. PostgreSQL was already chosen (pgx driver in go.mod, build-tag-gated PostgresStore).

## Decision

Use PostgreSQL with JSONB columns for roster persistence.

### Schema
- **players**: `id SERIAL`, `token TEXT UNIQUE` — one row per authenticated player
- **commanders**: `id SERIAL`, `player_id FK → players`, `name TEXT`, `type TEXT`, `level SMALLINT`, `gold INT`, `formation JSONB`, `combat_units JSONB` — one row per commander
- Unique constraint on `(player_id, name)` — commander names are unique per player, used for upsert

### Build tags
- Default build (`go build`): uses `postgres_nopgx.go` stub — all PostgresStore methods return errors
- Production build (`go build -tags pgx`): uses `postgres.go` with full pgx/pool implementation
- `main.go` checks `DATABASE_URL` env var: if set → PostgresStore, if absent → MockStore

### Why JSONB for formation/combat_units
- Formation template and combat unit arrays are always read/written as a whole (never query individual units)
- JSONB avoids schema migrations when Commander/CombatUnit fields change
- Go-side marshaling/unmarshaling preserves type safety

### Upsert strategy
`SaveCommander` uses `ON CONFLICT (player_id, name) DO UPDATE` — commander name is the natural key. This supports:
- Updating an existing commander's gold/level/units after a match
- Adding a new commander with a unique name

### Auto-schema
`ensureSchema()` runs `CREATE TABLE IF NOT EXISTS` on connect. Safe for first run and idempotent. The separate `migrations/001_init.sql` is for manual DBA use.

## Consequences

- **Pro**: Rosters survive restarts. JSONB avoids schema churn. Build tags keep CI fast (no Postgres needed).
- **Con**: Two build targets to maintain. Commander names must be unique per player. No transactional multi-commander writes (each SaveCommander is independent).
