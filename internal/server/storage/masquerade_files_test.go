package store

import (
	"strings"
	"testing"
	"time"

	"laz/internal/server/model"
)

func TestMasqueradeSetsEncryptedAndPinnedUntilACK(t *testing.T) {
	raw, err := OpenSQLite(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	testMasqueradeFileSets(t, raw)
}

func testMasqueradeFileSets(t *testing.T, raw *SQLStore) {
	t.Helper()
	st, err := WrapSecrets(raw, "secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"old", "new", "draft"} {
		if err := st.PutHysteriaFileSet(model.HysteriaFileSet{ID: id, Archive: "private-archive", ManifestJSON: `{"files":[]}`}); err != nil {
			t.Fatal(err)
		}
	}
	encrypted, _ := raw.GetHysteriaFileSet("old")
	if !strings.HasPrefix(encrypted.Archive, "enc:v1:") {
		t.Fatal("unencrypted archive")
	}
	plain, _ := st.GetHysteriaFileSet("old")
	if plain.Archive != "private-archive" {
		t.Fatal("archive not decrypted")
	}
	node, err := st.CreateNode(model.Node{Name: "H2", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateHysteria2NodeConfig(model.Hysteria2NodeConfig{NodeID: node.ID, EndpointHost: "h2.example.com", MasqueradeFileSetID: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetHysteria2ConfigResult(node.ID, c.ConfigVersionMS, ""); err != nil {
		t.Fatal(err)
	}
	expected := c.ConfigVersionMS
	c.ConfigVersionMS++
	c.MasqueradeFileSetID = "new"
	if err = st.UpdateHysteria2NodeConfig(c, expected, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = raw.exec(`update hysteria_masquerade_filesets set created_at_ms=0`); err != nil {
		t.Fatal(err)
	}
	if err = st.PruneHysteriaFileSets(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = st.GetHysteriaFileSet("draft"); err != ErrNotFound {
		t.Fatal("draft was not pruned")
	}
	for _, id := range []string{"old", "new"} {
		if _, err = st.GetHysteriaFileSet(id); err != nil {
			t.Fatal("referenced set pruned", id)
		}
	}
	if err = st.SetHysteria2ConfigResult(node.ID, c.ConfigVersionMS, ""); err != nil {
		t.Fatal(err)
	}
	if err = st.PruneHysteriaFileSets(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = st.GetHysteriaFileSet("old"); err != ErrNotFound {
		t.Fatal("old set retained after ACK")
	}
}
