//go:build !pgx

package persist

import (
	"context"
	"fmt"
)

// PostgresStore stub for builds without pgx dependency.
// Use -tags nopgx to include this stub instead of the real implementation.
type PostgresStore struct{}

func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	return &PostgresStore{}, nil
}

func (s *PostgresStore) Close() {}

func (s *PostgresStore) FindOrCreatePlayer(ctx context.Context, token string) (*Player, error) {
	return nil, fmt.Errorf("pgx not available: rebuild without -tags nopgx")
}

func (s *PostgresStore) LoadRoster(ctx context.Context, playerID uint32) ([]Commander, error) {
	return nil, fmt.Errorf("pgx not available")
}

func (s *PostgresStore) SaveCommander(ctx context.Context, playerID uint32, cmd Commander) error {
	return fmt.Errorf("pgx not available")
}

func (s *PostgresStore) DeleteCommander(ctx context.Context, playerID uint32, cmdID uint8) error {
	return fmt.Errorf("pgx not available")
}

func (s *PostgresStore) CreateStarterRoster(ctx context.Context, playerID uint32) error {
	return fmt.Errorf("pgx not available")
}
