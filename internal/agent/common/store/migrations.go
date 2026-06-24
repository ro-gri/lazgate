package store

import (
	"database/sql"
	"embed"
	"fmt"
	"laz/internal/persistence"

	commonmigrations "laz/internal/persistence/migrations"
)

//go:embed migrations/sql/*.sql
var migrationFS embed.FS

const migrationTable = "agent_store_goose_db_version"

func applyMigrations(db *sql.DB, dialect persistence.Dialect) error {
	dir, err := migrationDir(dialect)
	if err != nil {
		return err
	}
	return commonmigrations.ApplyWithTable(db, migrationFS, dir, dialect, migrationTable)
}

func migrationDir(dialect persistence.Dialect) (string, error) {
	switch dialect {
	case persistence.DialectSQLite, persistence.DialectPostgres:
		return "migrations/sql", nil
	default:
		return "", fmt.Errorf("unsupported agent store migration dialect %q", dialect)
	}
}
