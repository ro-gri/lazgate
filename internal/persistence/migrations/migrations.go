package migrations

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sync"

	"github.com/pressly/goose/v3"
)

type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DefaultTable    string  = "goose_db_version"
)

var gooseMu sync.Mutex

func Apply(db *sql.DB, fsys fs.FS, dir string, dialect Dialect) error {
	return ApplyWithTable(db, fsys, dir, dialect, DefaultTable)
}

func ApplyWithTable(db *sql.DB, fsys fs.FS, dir string, dialect Dialect, table string) error {
	gooseMu.Lock()
	defer gooseMu.Unlock()

	if table == "" {
		table = DefaultTable
	}
	goose.SetBaseFS(fsys)
	goose.SetTableName(table)
	switch dialect {
	case DialectSQLite:
		if err := goose.SetDialect("sqlite3"); err != nil {
			return err
		}
	case DialectPostgres:
		if err := goose.SetDialect("postgres"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported migration dialect %q", dialect)
	}
	return goose.Up(db, dir)
}
