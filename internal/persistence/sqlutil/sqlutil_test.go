package sqlutil

import (
	"laz/internal/persistence"
	"testing"
)

func TestRebinder(t *testing.T) {
	for _, tc := range []struct {
		dialect persistence.Dialect
		want    string
	}{{persistence.DialectSQLite, "select ? + ?"}, {persistence.DialectPostgres, "select $1 + $2"}} {
		bind, err := NewRebinder(tc.dialect)
		if err != nil {
			t.Fatal(err)
		}
		if got := bind("select ? + ?"); got != tc.want {
			t.Fatalf("%s: %s", tc.dialect, got)
		}
	}
	if bind, err := NewRebinder("invalid"); err == nil || bind != nil {
		t.Fatal("unsupported dialect accepted")
	}
}
