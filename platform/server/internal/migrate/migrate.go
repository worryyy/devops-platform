package migrate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	// register the pgx5:// database driver with golang-migrate
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"

	"github.com/worryyy/devops-platform/platform/server/migrations"
)

// Up applies all pending migrations embedded in the server binary.
// Already-current schema is a success (ErrNoChange).
func Up(databaseURL string) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("open migration source: %w", err)
	}

	// golang-migrate picks its driver by URL scheme; pgx5 keeps a single
	// postgres driver in the binary (the same one pgxpool uses).
	url := strings.TrimSpace(databaseURL)
	switch {
	case strings.HasPrefix(url, "postgres://"):
		url = "pgx5://" + strings.TrimPrefix(url, "postgres://")
	case strings.HasPrefix(url, "postgresql://"):
		url = "pgx5://" + strings.TrimPrefix(url, "postgresql://")
	case strings.HasPrefix(url, "pgx5://"):
		// already in the expected scheme
	default:
		return fmt.Errorf("unsupported DATABASE_URL scheme (want postgres:// or pgx5://)")
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, url)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
