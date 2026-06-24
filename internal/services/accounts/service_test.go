package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"laz/internal/model"
	"laz/internal/services/connections"
	"laz/internal/storage"
)

type fakeConnectionCreator struct {
	nodes   []model.Node
	results map[string]connections.ConnectionResult
	errs    map[string]error
}

func (f fakeConnectionCreator) SelectEnrollmentNodes(string, []string) ([]model.Node, error) {
	return f.nodes, nil
}

func (f fakeConnectionCreator) CreateEnrollmentConnection(_ context.Context, node model.Node, _ model.Account, _ model.Client, _, _ int) (connections.ConnectionResult, error) {
	if err := f.errs[node.ID]; err != nil {
		return connections.ConnectionResult{}, err
	}
	return f.results[node.ID], nil
}

func TestCreateAccountValidation(t *testing.T) {
	err := CreateAccountInput{}.Validate()
	var validation ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got %T %[1]v", err)
	}
}

func TestEnrollCreatesUserDeviceAndCollectsNodeResults(t *testing.T) {
	st := newTestStore(t)
	okNode, err := st.CreateNode(model.Node{Name: "hy2", Type: model.NodeTypeBlitzHysteria})
	if err != nil {
		t.Fatal(err)
	}
	failNode, err := st.CreateNode(model.Node{Name: "awg", Type: model.NodeTypeAmneziaAPI})
	if err != nil {
		t.Fatal(err)
	}
	connectionCreator := fakeConnectionCreator{
		nodes: []model.Node{okNode, failNode},
		results: map[string]connections.ConnectionResult{
			okNode.ID: {
				Connection: model.Connection{ID: "acc_1"},
				Configs: []model.IssuedConfig{
					{ID: "cfg_1"},
					{ID: "cfg_2"},
				},
			},
		},
		errs: map[string]error{failNode.ID: errors.New("remote failed")},
	}
	svc := New(st, connectionCreator)

	result, err := svc.Enroll(context.Background(), EnrollmentInput{
		Username:   "rogri",
		ClientSlug: "mac",
		ClientName: "Mac",
		Nodes:      "all",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Account.Username != "rogri" || result.Client.Slug != "mac" {
		t.Fatalf("unexpected enrollment identity %#v", result)
	}
	if result.Successes != 1 || !result.Partial {
		t.Fatalf("expected one success and partial result, got %#v", result)
	}
	if len(result.Results) != 2 || result.Results[0].ConfigCount != 2 || result.Results[1].Err == nil {
		t.Fatalf("unexpected node results %#v", result.Results)
	}
	if result.SummaryErr != nil || result.Summary.Account.ID != result.Account.ID {
		t.Fatalf("expected summary for enrolled account, got summary=%#v err=%v", result.Summary, result.SummaryErr)
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
