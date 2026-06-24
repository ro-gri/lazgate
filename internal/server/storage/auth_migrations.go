package store

import (
	"database/sql"
	"embed"
	"laz/internal/persistence"

	commonmigrations "laz/internal/persistence/migrations"
)

const authMigrationDir = "authmigrations/sql"

//go:embed authmigrations/sql/*.sql
var authMigrationFS embed.FS

func applyAuthMigrations(db *sql.DB, dialect persistence.Dialect) error {
	return commonmigrations.Apply(db, authMigrationFS, authMigrationDir, dialect)
}
