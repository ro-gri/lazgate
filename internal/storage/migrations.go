package store

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

const migrationDir = "migrations/sql"

//go:embed migrations/sql/*.sql
var migrationFS embed.FS

func applyMigrations(db *sql.DB, dialect SQLDialect) error {
	goose.SetBaseFS(migrationFS)
	switch dialect {
	case dialectSQLite:
		if err := goose.SetDialect("sqlite3"); err != nil {
			return err
		}
	case dialectPostgres:
		if err := goose.SetDialect("postgres"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	return goose.Up(db, migrationDir)
}
