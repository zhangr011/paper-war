-- Paper War: Initial Schema
-- Creates players and commanders tables for roster persistence.
-- Run manually or via ensureSchema() in the application.

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
    formation    JSONB NOT NULL DEFAULT '{"weapon_slot":"Light","armor_slot":"Light","leading_skill":100}',
    combat_units JSONB NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_commanders_player_name UNIQUE (player_id, name)
);

CREATE INDEX IF NOT EXISTS idx_commanders_player_id ON commanders(player_id);
CREATE INDEX IF NOT EXISTS idx_players_token ON players(token);
