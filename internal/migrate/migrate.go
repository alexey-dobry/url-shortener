package migrate

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/example/url-shortener/internal/storage"
)

type Migration struct {
	Version     int
	Description string
	SQL         string
}

var Migrations = []Migration{
	{
		Version:     1,
		Description: "create users table",
		SQL: `
			CREATE TABLE IF NOT EXISTS users (
				id            BIGSERIAL PRIMARY KEY,
				username      TEXT NOT NULL UNIQUE,
				password_hash TEXT NOT NULL,
				created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);`,
	},
	{
		Version:     2,
		Description: "create links table",
		SQL: `
			CREATE TABLE IF NOT EXISTS links (
				code        TEXT PRIMARY KEY,
				url         TEXT NOT NULL,
				owner_id    BIGINT NOT NULL REFERENCES users(id),
				clicks      BIGINT NOT NULL DEFAULT 0,
				created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);`,
	},
}

func Up(ctx context.Context, store storage.Storage, log *slog.Logger) error {
	if err := store.ExecRaw(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := store.QueryVersions(ctx,
		`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read applied versions: %w", err)
	}
	appliedSet := make(map[int]bool, len(applied))
	for _, v := range applied {
		appliedSet[v] = true
	}

	sorted := make([]Migration, len(Migrations))
	copy(sorted, Migrations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	count := 0
	for _, m := range sorted {
		if appliedSet[m.Version] {
			log.Debug("migration already applied, skipping",
				slog.Int("version", m.Version),
				slog.String("description", m.Description),
			)
			continue
		}

		log.Info("applying migration",
			slog.Int("version", m.Version),
			slog.String("description", m.Description),
		)
		if err := store.ExecRaw(ctx, m.SQL); err != nil {
			return fmt.Errorf("migration v%d (%s): %w", m.Version, m.Description, err)
		}
		if err := store.ExecRaw(ctx,
			fmt.Sprintf(`INSERT INTO schema_migrations (version, description) VALUES (%d, '%s')`,
				m.Version, m.Description)); err != nil {
			return fmt.Errorf("record migration v%d: %w", m.Version, err)
		}
		count++
	}

	log.Info("migrations complete", slog.Int("applied", count), slog.Int("total", len(sorted)))
	return nil
}
