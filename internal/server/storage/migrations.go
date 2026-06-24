package store

import (
	"database/sql"
	"embed"
	"laz/internal/persistence"

	commonmigrations "laz/internal/persistence/migrations"
)

const migrationDir = "migrations/sql"

//go:embed migrations/sql/*.sql
var migrationFS embed.FS

func applyMigrations(db *sql.DB, dialect persistence.Dialect) error {
	return commonmigrations.Apply(db, migrationFS, migrationDir, dialect)
}
