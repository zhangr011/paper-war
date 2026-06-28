-- Paper War v1 Database Schema
-- Run with: psql -d paperwar -f schema.sql

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS players (
    id         SERIAL PRIMARY KEY,
    token      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS commanders (
    id           SERIAL PRIMARY KEY,
    player_id    INT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT 'Commander',
    type         TEXT NOT NULL DEFAULT 'LightInfantry',
    level        SMALLINT NOT NULL DEFAULT 1,
    gold         INT NOT NULL DEFAULT 50,
    formation    JSONB NOT NULL DEFAULT '{"weapon_slot":"Light","armor_slot":"Light","leading_skill":7}',
    combat_units JSONB NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_commanders_player_id ON commanders(player_id);
CREATE INDEX IF NOT EXISTS idx_players_token ON players(token);

-- v1.1: Career stats accumulator. One row per player, updated atomically
-- at each match end via ON CONFLICT DO UPDATE. Separate from commanders
-- (mutable roster) to keep career totals append-mostly.
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
