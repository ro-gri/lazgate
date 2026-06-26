package store

import (
	"database/sql"
	"embed"
	"fmt"

	commonmigrations "laz/internal/persistence/migrations"
)

const migrationDir = "migrations/sql"

//go:embed migrations/sql/*.sql
var migrationFS embed.FS

func applyMigrations(db *sql.DB, dialect SQLDialect) error {
	var commonDialect commonmigrations.Dialect
	switch dialect {
	case dialectSQLite:
		commonDialect = commonmigrations.DialectSQLite
	case dialectPostgres:
		commonDialect = commonmigrations.DialectPostgres
	default:
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	return commonmigrations.Apply(db, migrationFS, migrationDir, commonDialect)
}
