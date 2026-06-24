package sqlutil

import (
	"laz/internal/persistence"

	"github.com/jmoiron/sqlx"
)

// NewRebinder validates the backend once, when opening the store.
func NewRebinder(dialect persistence.Dialect) (func(string) string, error) {
	if err := dialect.Validate(); err != nil {
		return nil, err
	}
	if dialect == persistence.DialectPostgres {
		return func(query string) string { return sqlx.Rebind(sqlx.DOLLAR, query) }, nil
	}
	return func(query string) string { return query }, nil
}
