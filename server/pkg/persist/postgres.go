//go:build pgx

package persist

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store using PostgreSQL with JSONB columns.
// Build with -tags nopgx to use a stub instead (for CI without Postgres).
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a connection pool and ensures the schema exists.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("parse conn string: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	ps := &PostgresStore{pool: pool}
	if err := ps.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}
	return ps, nil
}

func (s *PostgresStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) ensureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
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
	`)
	return err
}

func (s *PostgresStore) FindOrCreatePlayer(ctx context.Context, token string) (*Player, error) {
	// Try to find existing player
	var id uint32
	err := s.pool.QueryRow(ctx, `SELECT id FROM players WHERE token = $1`, token).Scan(&id)
	if err == nil {
		roster, err := s.LoadRoster(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load roster for existing player: %w", err)
		}
		return &Player{ID: id, Token: token, Commanders: roster}, nil
	}

	// Insert new player
	err = s.pool.QueryRow(ctx,
		`INSERT INTO players (token, created_at) VALUES ($1, $2) RETURNING id`,
		token, time.Now(),
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert player: %w", err)
	}

	// Create starter roster
	if err := s.CreateStarterRoster(ctx, id); err != nil {
		return nil, fmt.Errorf("create starter roster: %w", err)
	}

	roster, err := s.LoadRoster(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load starter roster: %w", err)
	}
	return &Player{ID: id, Token: token, Commanders: roster}, nil
}

func (s *PostgresStore) LoadRoster(ctx context.Context, playerID uint32) ([]Commander, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, type, level, gold, formation, combat_units
		FROM commanders WHERE player_id = $1 ORDER BY id
	`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commanders []Commander
	cmdID := uint8(0)
	for rows.Next() {
		cmdID++
		var dbID int
		var name, cmdType string
		var level int
		var gold int
		var formationJSON, unitsJSON []byte

		if err := rows.Scan(&dbID, &name, &cmdType, &level, &gold, &formationJSON, &unitsJSON); err != nil {
			return nil, err
		}

		var formation FormationTemplate
		if err := json.Unmarshal(formationJSON, &formation); err != nil {
			return nil, fmt.Errorf("unmarshal formation: %w", err)
		}

		var units []CombatUnit
		if err := json.Unmarshal(unitsJSON, &units); err != nil {
			return nil, fmt.Errorf("unmarshal units: %w", err)
		}

		commanders = append(commanders, Commander{
			ID:        cmdID,
			Name:      name,
			Type:      cmdType,
			Level:     uint8(level),
			Gold:      int32(gold),
			Formation: formation,
			Units:     units,
		})
	}
	return commanders, rows.Err()
}

func (s *PostgresStore) SaveCommander(ctx context.Context, playerID uint32, cmd Commander) error {
	formationJSON, err := json.Marshal(cmd.Formation)
	if err != nil {
		return fmt.Errorf("marshal formation: %w", err)
	}
	unitsJSON, err := json.Marshal(cmd.Units)
	if err != nil {
		return fmt.Errorf("marshal units: %w", err)
	}

	// Upsert: insert or update on conflict (player_id, name)
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO commanders (player_id, name, type, level, gold, formation, combat_units, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (player_id, name) DO UPDATE SET
			type = EXCLUDED.type,
			level = EXCLUDED.level,
			gold = EXCLUDED.gold,
			formation = EXCLUDED.formation,
			combat_units = EXCLUDED.combat_units
	`, playerID, cmd.Name, cmd.Type, int(cmd.Level), int(cmd.Gold), formationJSON, unitsJSON, time.Now())
	if err != nil {
		return fmt.Errorf("upsert commander: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("save commander: no rows affected")
	}
	return nil
}

func (s *PostgresStore) DeleteCommander(ctx context.Context, playerID uint32, cmdID uint8) error {
	// Delete by offset — cmdID is 1-based index in the roster
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM commanders
		WHERE id = (
			SELECT id FROM commanders
			WHERE player_id = $1
			ORDER BY id
			OFFSET $2 LIMIT 1
		)
	`, playerID, int(cmdID-1))
	if err != nil {
		return fmt.Errorf("delete commander: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("commander %d not found for player %d", cmdID, playerID)
	}
	return nil
}

func (s *PostgresStore) CreateStarterRoster(ctx context.Context, playerID uint32) error {
	units := make([]CombatUnit, 5)
	for i := range units {
		units[i] = CombatUnit{
			ID:    uint8(i + 1),
			Type:  "LightInfantry",
			Level: 1,
		}
	}

	formation := FormationTemplate{
		WeaponSlot:   "Light",
		ArmorSlot:    "Light",
		LeadingSkill: 100,
	}

	formationJSON, err := json.Marshal(formation)
	if err != nil {
		return err
	}
	unitsJSON, err := json.Marshal(units)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO commanders (player_id, name, type, level, gold, formation, combat_units, created_at)
		VALUES ($1, 'Starter Commander', 'LightInfantry', 1, 50, $2, $3, $4)
	`, playerID, formationJSON, unitsJSON, time.Now())
	return err
}
