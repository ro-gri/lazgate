package workqueue

import (
	"context"
	"testing"

	transportstore "laz/internal/queue"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func TestAuthRefreshFinalizesConnectionOnlyAfterAppliedAck(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/laz.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	account, err := st.CreateAccount(model.Account{Username: "ack", DisplayName: "Ack"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "ack-node", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := st.CreateConnectionBundle(model.ConnectionBundle{
		Connection:        model.Connection{AccountID: account.ID, NodeID: node.ID, Name: "Phone", Protocol: model.ProtocolHysteria2},
		Hysteria2Material: &model.Hysteria2ConnectionMaterial{Password: "secret", PasswordHash: "secret-hash"},
		Configs:           []model.IssuedConfig{{Kind: model.ConfigHy2URI, Name: "Phone", Config: "hy2://secret@example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection := bundle.Connection
	if !connection.Pending() || connection.AppliedStatus != model.ConnectionStatusAbsent {
		t.Fatalf("connection must remain pending before ACK: %+v", connection)
	}

	called := false
	handler := AuthRefreshHandler{
		Store: st,
		Refresher: AuthRefresherFunc(func(_ context.Context, nodeID, accountID string, version int64) (int64, error) {
			called = true
			if nodeID != node.ID || accountID != account.ID || version != connection.DesiredVersionMS {
				t.Fatalf("unexpected refresh request: node=%s account=%s version=%d", nodeID, accountID, version)
			}
			return version, nil
		}),
	}
	result, err := handler.Handle(context.Background(), NewAuthRefreshMessage(AuthRefreshPayload{NodeID: node.ID, AccountID: account.ID, SnapshotVersionMS: connection.DesiredVersionMS}))
	if err != nil {
		t.Fatal(err)
	}
	if !called || result.Status != transportstore.StatusApplied || len(result.Events) != 2 {
		t.Fatalf("unexpected auth refresh result: called=%v result=%+v", called, result)
	}
	applied, err := st.GetConnection(connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Pending() || applied.AppliedStatus != model.ConnectionStatusActive || applied.AppliedVersionMS != applied.DesiredVersionMS {
		t.Fatalf("ACK was not finalized: %+v", applied)
	}
	for _, event := range result.Events {
		if event.EntityType != "connection" || event.EntityID != connection.ID || event.Type != "connection.applied" {
			t.Fatalf("unexpected event: %+v", event)
		}
	}
}
