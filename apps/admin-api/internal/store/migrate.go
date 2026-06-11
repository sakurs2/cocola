package store

import (
	"context"
	"database/sql"

	cocoladb "github.com/cocola-project/cocola/db"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx" for goose
	"github.com/pressly/goose/v3"
)

// Migrate applies all pending goose migrations from the embedded db module to
// the database at dsn. It is idempotent: goose records applied versions in its
// goose_db_version table, so re-running is a no-op. The schema in db/migrations
// is the single source of truth shared with the Python services.
//
// We open a short-lived database/sql handle via the pgx stdlib driver (goose
// speaks database/sql, not pgxpool) purely for migration, then close it; the
// app's own queries go through the pgxpool in NewPostgres.
func Migrate(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(cocoladb.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, cocoladb.MigrationsDir)
}
