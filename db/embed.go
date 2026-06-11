// Package db is the single source of truth for cocola's relational schema.
//
// The SQL migration files in migrations/ are written in goose format and are
// the ONLY place the schema is defined. Go services embed this FS and apply it
// with goose at startup; Python services (llm-gateway) connect to the SAME
// database with psycopg and assume goose has already applied these files -- they
// never (re)declare schema. See docs/plan/m7-persistence-postgres.md.
package db

import "embed"

// Migrations holds the goose-format SQL migration files. Use Migrations as a
// goose filesystem source: goose.SetBaseFS(db.Migrations) with dir "migrations".
//
//go:embed migrations/*.sql
var Migrations embed.FS

// MigrationsDir is the sub-directory inside Migrations that holds the .sql files.
const MigrationsDir = "migrations"
