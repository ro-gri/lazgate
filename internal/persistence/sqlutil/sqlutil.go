package sqlutil

import (
	commonmigrations "laz/internal/persistence/migrations"

	"github.com/jmoiron/sqlx"
)

func Rebind(query string, dialect commonmigrations.Dialect) string {
	switch dialect {
	case commonmigrations.DialectPostgres:
		return sqlx.Rebind(sqlx.DOLLAR, query)
	default:
		return query
	}
}
