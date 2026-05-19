//go:build !nopgx

package persist

import (
	"context"
	"fmt"
)

// PostgresStore implements Store using PostgreSQL with JSONB columns.
// The real implementation requires the pgx dependency.
// Build with -tags nopgx to skip this file.
type PostgresStore struct{}

func NewPostgresStore(connString string) (*PostgresStore, error) {
	return nil, fmt.Errorf("PostgresStore requires pgx dependency; build without -tags nopgx")
}

func (s *PostgresStore) Close() {}

func (s *PostgresStore) FindOrCreatePlayer(ctx context.Context, token string) (*Player, error) {
	return nil, fmt.Errorf("not implemented without pgx")
}

func (s *PostgresStore) LoadRoster(ctx context.Context, playerID uint32) ([]Commander, error) {
	return nil, fmt.Errorf("not implemented without pgx")
}

func (s *PostgresStore) SaveCommander(ctx context.Context, playerID uint32, cmd Commander) error {
	return fmt.Errorf("not implemented without pgx")
}

func (s *PostgresStore) DeleteCommander(ctx context.Context, playerID uint32, cmdID uint8) error {
	return fmt.Errorf("not implemented without pgx")
}

func (s *PostgresStore) CreateStarterRoster(ctx context.Context, playerID uint32) error {
	return fmt.Errorf("not implemented without pgx")
}
