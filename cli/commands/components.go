package commands

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
)

func runMigrations(ctx context.Context, dir string, pool *pgxpool.Pool) error {
	c, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}

	defer c.Release()

	m, err := migrate.NewMigrator(ctx, c.Conn(), "schema_versions")
	if err != nil {
		return err
	}

	err = m.LoadMigrations(os.DirFS(dir))
	if err != nil {
		return err
	}

	return m.Migrate(ctx)
}
