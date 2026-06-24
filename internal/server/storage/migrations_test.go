package store

import (
	"database/sql"
	"io/fs"
	"testing"
)

func TestFreshDatabaseBaseline(t *testing.T) {
	for _, source := range []struct {
		name   string
		dir    string
		source fs.FS
	}{
		{"main", migrationDir, migrationFS},
		{"auth", authMigrationDir, authMigrationFS},
	} {
		items, err := fs.ReadDir(source.source, source.dir)
		if err != nil || len(items) != 1 {
			t.Fatalf("%s: expected one initial migration, count=%d err=%v", source.name, len(items), err)
		}
	}
	dir := t.TempDir()
	st, err := OpenSQLiteWithAuth(dir+"/main.db", dir+"/auth.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for name, db := range map[string]*sql.DB{"main": st.db, "auth": st.authDB} {
		var count, maxVersion int
		if err := db.QueryRow(`select count(*), max(version_id) from goose_db_version where is_applied = true and version_id > 0`).Scan(&count, &maxVersion); err != nil || count != 1 || maxVersion != 1 {
			t.Fatalf("%s: invalid migration baseline count=%d version=%d err=%v", name, count, maxVersion, err)
		}
		rows, err := db.Query(`pragma foreign_key_check`)
		if err != nil {
			t.Fatal(err)
		}
		if rows.Next() {
			rows.Close()
			t.Fatalf("%s: foreign key violation", name)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	profiles := st.ListConfigProfiles()
	if len(profiles) != 3 {
		t.Fatalf("expected three seeded profiles, got %d", len(profiles))
	}
}
