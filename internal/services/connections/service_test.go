package connections

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"laz/internal/model"
	"laz/internal/services/connections/remote"
	"laz/internal/storage"
)

type fakeProvider struct {
	created remote.CreateInput
	deleted []remote.Ref
	result  remote.Connection
}

func (f *fakeProvider) CreateConnection(_ context.Context, input remote.CreateInput) (remote.Connection, error) {
	f.created = input
	if f.result.Ref.ID == "" && f.result.Ref.Name == "" {
		f.result.Ref = remote.Ref{ID: input.Name, Name: input.Name}
	}
	return f.result, nil
}

func (f *fakeProvider) SetConnectionStatus(context.Context, remote.Ref, model.Status) error {
	return nil
}

func (f *fakeProvider) DeleteConnection(_ context.Context, ref remote.Ref) error {
	f.deleted = append(f.deleted, ref)
	return nil
}

func (f *fakeProvider) ListConnections(context.Context) (remote.ConnectionList, error) {
	return remote.ConnectionList{}, nil
}

func TestRemoteNameForUsesAccountIDSuffix(t *testing.T) {
	got := RemoteNameFor("rogri", "mac", "usr_1234567890abcdef")
	if got != "rogri_mac_12345678" {
		t.Fatalf("expected readable unique remote name, got %q", got)
	}
}

func TestSelectEnrollmentNodesAllOnlySupportedActiveNodes(t *testing.T) {
	st := newTestStore(t)
	activeBlitz, err := st.CreateNode(model.Node{Name: "hy2", Type: model.NodeTypeBlitzHysteria})
	if err != nil {
		t.Fatal(err)
	}
	activeAmnezia, err := st.CreateNode(model.Node{Name: "awg", Type: model.NodeTypeAmneziaAPI})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNode(model.Node{Name: "native", Type: model.NodeTypeNativeHy2}); err != nil {
		t.Fatal(err)
	}
	svc := New(st, func() (string, error) { return "pw", nil })
	nodes, err := svc.SelectEnrollmentNodes("all", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected two supported active nodes, got %#v", nodes)
	}
	got := map[string]bool{}
	for _, node := range nodes {
		got[node.ID] = true
	}
	if !got[activeBlitz.ID] || !got[activeAmnezia.ID] {
		t.Fatalf("expected selected nodes to include active Blitz and Amnezia nodes, got %#v", nodes)
	}
}

func TestCreateEnrollmentConnectionCreatesBlitzAccessAndConfigs(t *testing.T) {
	st := newTestStore(t)
	account, client, node := newConnectionsFixtures(t, st)
	fake := &fakeProvider{
		result: remote.Connection{
			Configs: []remote.Config{
				{
					Kind:        model.ConfigHy2URI,
					Slug:        "fi-ipv4",
					Name:        "FI IPv4",
					Client:      "happ",
					ContentType: "text/plain; charset=utf-8",
					Value:       "hy2://account@example.com:443?pinSHA256=abc#old",
				},
			},
		},
	}
	svc := New(st, func() (string, error) { return "generated-password", nil })
	svc.providerFor = func(model.Node) (remote.Provider, error) { return fake, nil }

	result, err := svc.CreateEnrollmentConnection(context.Background(), node, account, client, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(fake.created.Name, "rogri_mac_") || strings.HasSuffix(fake.created.Name, "_hy2") {
		t.Fatalf("unexpected remote name %q", fake.created.Name)
	}
	if fake.created.Password != "generated-password" || !fake.created.Unlimited {
		t.Fatalf("unexpected remote create input %#v", fake.created)
	}
	if result.Connection.Protocol != model.ProtocolHysteria2 || result.Connection.RemoteName != fake.created.Name {
		t.Fatalf("unexpected connection %#v", result.Connection)
	}
	if result.Configs[0].Slug != "fi-ipv4" {
		t.Fatalf("expected config slug from node URI name, got %#v", result.Configs[0])
	}
}

func TestCreateEnrollmentConnectionCompensatesBlitzUserOnDuplicateConnection(t *testing.T) {
	st := newTestStore(t)
	account, client, node := newConnectionsFixtures(t, st)
	if _, err := st.CreateConnection(model.Connection{
		AccountID: account.ID,
		ClientID:  client.ID,
		NodeID:    node.ID,
		Protocol:  model.ProtocolHysteria2,
	}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{
		result: remote.Connection{
			Ref: remote.Ref{ID: "rogri_mac_hy2", Name: "rogri_mac_hy2"},
			Configs: []remote.Config{{
				Kind:        model.ConfigHy2URI,
				Slug:        "fi-ipv4",
				Name:        "FI IPv4",
				Client:      "happ",
				ContentType: "text/plain; charset=utf-8",
				Value:       "hy2://account@example.com:443",
			}},
		},
	}
	svc := New(st, func() (string, error) { return "generated-password", nil })
	svc.providerFor = func(model.Node) (remote.Provider, error) { return fake, nil }

	_, err := svc.CreateEnrollmentConnection(context.Background(), node, account, client, 0, 0)
	if err == nil {
		t.Fatal("expected duplicate connection error")
	}
	if len(fake.deleted) != 1 || fake.deleted[0].Name != "rogri_mac_hy2" {
		t.Fatalf("expected remote Blitz account compensation, got %#v", fake.deleted)
	}
}

func newTestStore(t *testing.T) *store.SQLStore {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func newConnectionsFixtures(t *testing.T, st store.Store) (model.Account, model.Client, model.Node) {
	t.Helper()
	account, err := st.CreateAccount(model.Account{Username: "rogri"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := st.CreateClient(model.Client{AccountID: account.ID, Slug: "mac", Name: "Mac"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "FI Hysteria2", Type: model.NodeTypeBlitzHysteria})
	if err != nil {
		t.Fatal(err)
	}
	return account, client, node
}
