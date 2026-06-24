package migrations

import (
	"context"
	"database/sql"
	"io/fs"
	"laz/internal/persistence"

	"github.com/pressly/goose/v3"
)

const DefaultTable = "goose_db_version"

func Apply(db *sql.DB, fsys fs.FS, dir string, dialect persistence.Dialect) error {
	return ApplyWithTable(db, fsys, dir, dialect, DefaultTable)
}

// ApplyWithTable must be called sequentially for migration sets sharing a database.
func ApplyWithTable(db *sql.DB, fsys fs.FS, dir string, dialect persistence.Dialect, table string) error {
	if err := dialect.Validate(); err != nil {
		return err
	}
	if table == "" {
		table = DefaultTable
	}
	source, err := fs.Sub(fsys, dir)
	if err != nil {
		return err
	}
	var backend goose.Dialect
	switch dialect {
	case persistence.DialectSQLite:
		backend = goose.DialectSQLite3
	case persistence.DialectPostgres:
		backend = goose.DialectPostgres
	}
	provider, err := goose.NewProvider(backend, db, source, goose.WithTableName(table), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	_, err = provider.Up(context.Background())
	return err
}
