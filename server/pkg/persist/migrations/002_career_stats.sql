-- v1.1 migration: add player_career table for cumulative cross-match stats.
-- Idempotent — safe to run on existing databases.
-- ensureSchema() in postgres.go also creates this table on first connect,
-- so this migration is for manual DBA use / existing deployments.

CREATE TABLE IF NOT EXISTS player_career (
    player_id          INT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    matches_played     INT NOT NULL DEFAULT 0,
    matches_won        INT NOT NULL DEFAULT 0,
    matches_lost       INT NOT NULL DEFAULT 0,
    total_kills        INT NOT NULL DEFAULT 0,
    total_deaths       INT NOT NULL DEFAULT 0,
    commander_kills    INT NOT NULL DEFAULT 0,
    commanders_lost    INT NOT NULL DEFAULT 0,
    total_gold_earned  INT NOT NULL DEFAULT 0,
    total_gold_spent   INT NOT NULL DEFAULT 0,
    total_recruits     INT NOT NULL DEFAULT 0,
    last_played_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
