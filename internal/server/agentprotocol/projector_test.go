package agentprotocol

import (
	"context"
	protocol "laz/internal/protocol"
	"testing"
)

func TestPageChangesSortsAndAppliesCredentialCursor(t *testing.T) {
	page, err := PageChanges([]protocol.Change[string]{{ConnectionID: "c"}, {ConnectionID: "a"}, {ConnectionID: "b"}}, "a", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].ConnectionID != "b" || page[1].ConnectionID != "c" {
		t.Fatalf("unexpected page: %#v", page)
	}
}

func TestStoredConnectionProjectorSelectsFullAndIncrementalCredentials(t *testing.T) {
	store := &storedCredentialSource{credentials: map[string][]StoredConnection[string]{
		"node-1": {{ConnectionID: "cred-1", Active: true, UpdatedAtMS: 100, Material: "one"}, {ConnectionID: "cred-2", Active: false, UpdatedAtMS: 300, Material: "two"}},
		"node-2": {{ConnectionID: "cred-3", Active: true, UpdatedAtMS: 400, Material: "three"}},
	}}
	projector := StoredConnectionProjector[string, string]{NodeID: "node-1", Source: store, Builder: StoredConnectionChangeBuilderFunc[string, string](func(_ context.Context, c StoredConnection[string]) (string, error) { return c.Material, nil })}
	full, err := projector.ListSyncChanges(context.Background(), Query{Full: true, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 1 || full[0].ConnectionID != "cred-1" || full[0].Operation != protocol.OperationUpsert {
		t.Fatalf("full changes = %#v", full)
	}
	incremental, err := projector.ListSyncChanges(context.Background(), Query{SinceManifestStartedAtMS: 250, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(incremental) != 1 || incremental[0].ConnectionID != "cred-2" || incremental[0].Operation != protocol.OperationDelete {
		t.Fatalf("incremental changes = %#v", incremental)
	}
}

type storedCredentialSource struct {
	credentials map[string][]StoredConnection[string]
}

func (s *storedCredentialSource) ListStoredConnections(_ context.Context, nodeID, fromConnectionID string) ([]StoredConnection[string], error) {
	var out []StoredConnection[string]
	for _, c := range s.credentials[nodeID] {
		if c.ConnectionID > fromConnectionID {
			out = append(out, c)
		}
	}
	return out, nil
}
