package clientselfservice

import (
	"context"
	"errors"
	"testing"

	"laz/internal/server/connections"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func TestConnectionPolicyLimitsSelfServiceAndAllowedNodes(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/laz.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	account, err := st.CreateAccount(model.Account{Username: "self-service", DisplayName: "Self Service"})
	if err != nil {
		t.Fatal(err)
	}
	nodeA := createHysteriaNode(t, st, "node-a")
	nodeB := createHysteriaNode(t, st, "node-b")
	deniedNode := createHysteriaNode(t, st, "node-denied")
	tag, err := st.CreatePolicyTag(model.PolicyTag{Slug: "two", Name: "Two connections", AllowedNodeIDs: []string{nodeA.ID, nodeB.ID}, ConnectionLimit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.AssignPolicyTag(model.AccountPolicyTag{AccountID: account.ID, TagID: tag.ID}); err != nil {
		t.Fatal(err)
	}

	manager := connections.New(st, func() (string, error) { return "generated-password", nil })
	service := New(st, manager)
	created, err := service.CreateConnections(context.Background(), CreateConnectionInput{AccountID: account.ID, ConnectionName: "Phone", NodeIDs: []string{nodeA.ID, nodeB.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if created.Partial || len(created.Results) != 2 {
		t.Fatalf("unexpected create result: %+v", created)
	}
	count, err := st.CountActiveConnectionsForAccount(account.ID)
	if err != nil || count != 2 {
		t.Fatalf("active connection count=%d err=%v", count, err)
	}

	_, err = service.CreateConnections(context.Background(), CreateConnectionInput{AccountID: account.ID, ConnectionName: "Tablet", NodeIDs: []string{nodeA.ID}})
	if !errors.Is(err, ErrConnectionLimitReached) {
		t.Fatalf("expected connection limit error, got %v", err)
	}
	_, err = service.CreateConnections(context.Background(), CreateConnectionInput{AccountID: account.ID, ConnectionName: "Denied", NodeIDs: []string{deniedNode.ID}})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden node error, got %v", err)
	}
}

func createHysteriaNode(t *testing.T, st *store.SQLStore, name string) model.Node {
	t.Helper()
	node, err := st.CreateNode(model.Node{Name: name, Type: model.NodeTypeNativeHy2, Region: "NL-Amsterdam"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.CreateHysteria2NodeConfig(model.Hysteria2NodeConfig{NodeID: node.ID, EndpointHost: name + ".example", EndpointPort: 443}); err != nil {
		t.Fatal(err)
	}
	return node
}
