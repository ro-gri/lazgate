package persistence

import "fmt"

type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
)

func (d Dialect) Validate() error {
	switch d {
	case DialectSQLite, DialectPostgres:
		return nil
	default:
		return fmt.Errorf("unsupported database dialect %q", d)
	}
}
