package store

import (
	"os"
	"testing"

	"laz/internal/server/model"
)

func TestPostgresMigrationsAndStorage(t *testing.T) {
	databaseURL := os.Getenv("LAZ_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set LAZ_TEST_POSTGRES_URL to run PostgreSQL storage tests")
	}

	st, err := OpenPostgres(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.db.Close() })

	for _, version := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		var applied bool
		err = st.db.QueryRow(`select is_applied from goose_db_version where version_id = $1`, version).Scan(&applied)
		if err != nil {
			t.Fatal(err)
		}
		if !applied {
			t.Fatalf("expected migration version %d to be applied", version)
		}
	}

	suffix := NewID("pgtest")
	account, err := st.CreateAccount(model.Account{Username: suffix, DisplayName: "Postgres Qwerty"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "iphone", Name: "iPhone"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateAccessToken(model.AccessToken{
		AccountID: account.ID,
		ClientID:  client.ID,
		Token:     "client-token",
		TokenHash: suffix + "-client-hash",
		Purpose:   model.TokenPurposeClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateShortLink(model.ShortLink{
		ID:           suffix + "-client-link",
		TokenID:      token.ID,
		Profile:      "all",
		TargetURL:    "https://net.example/c/" + suffix,
		EncryptedURL: "happ://client",
	}); err != nil {
		t.Fatal(err)
	}
}
