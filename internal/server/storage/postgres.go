package store

import (
	"database/sql"
	"errors"
	"laz/internal/persistence"
	"laz/internal/persistence/sqlutil"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func OpenPostgres(databaseURL string) (*SQLStore, error) {
	return openPostgres(databaseURL, "")
}

func OpenPostgresWithAuth(databaseURL, authDatabaseURL string) (*SQLStore, error) {
	if strings.TrimSpace(authDatabaseURL) == "" {
		return nil, errors.New("auth postgres database URL is required")
	}
	return openPostgres(databaseURL, authDatabaseURL)
}

func openPostgres(databaseURL, authDatabaseURL string) (*SQLStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	st := &SQLStore{db: db, dialect: persistence.DialectPostgres}
	st.rebinder, err = sqlutil.NewRebinder(st.dialect)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if authDatabaseURL != "" {
		authDB, err := sql.Open("pgx", authDatabaseURL)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		st.authDB = authDB
	}
	if err := st.migrate(); err != nil {
		_ = st.Close()
		return nil, err
	}
	return st, nil
}
