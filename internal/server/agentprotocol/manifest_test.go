package agentprotocol

import (
	"context"
	"errors"
	protocol "laz/internal/protocol"
	"reflect"
	"testing"

	commonproto "laz/generated/nodeproto/agentcommon"
)

func TestManifestResponseBuilderMapsCommonResponse(t *testing.T) {
	builder := protocol.NewManifestResponseBuilder(func(values protocol.ManifestResponseValues[string]) protocol.ManifestResponseValues[string] {
		return values
	})

	empty := builder.EmptySyncResponse()
	if !reflect.DeepEqual(empty, protocol.ManifestResponseValues[string]{}) {
		t.Fatalf("unexpected empty response: %+v", empty)
	}

	failed := builder.ErrorSyncResponse("node-1", true, errors.New("failed"))
	if failed.NodeID != "node-1" || !failed.Full || failed.Error != "failed" {
		t.Fatalf("unexpected error response: %+v", failed)
	}

	built := builder.BuildSyncResponse("node-1", protocol.Response[string]{
		ManifestStartedAtMS: 123,
		Full:                true,
		Changes: []protocol.Change[string]{
			{ConnectionID: "connection-1", Payload: "first"},
			{ConnectionID: "connection-2", Payload: "second"},
		},
	})
	if built.NodeID != "node-1" || built.ManifestStartedAtMS != 123 || !built.Full || !reflect.DeepEqual(built.Changes, []string{"first", "second"}) || built.Error != "" {
		t.Fatalf("unexpected built response: %+v", built)
	}
}

func TestNewStoredManifestHandlerWiresCommonFlow(t *testing.T) {
	builder := protocol.NewManifestResponseBuilder(func(values protocol.ManifestResponseValues[string]) protocol.ManifestResponseValues[string] {
		return values
	})
	handler := NewStoredManifestHandler(
		func(_ context.Context, nodeID, fromConnectionID string) ([]StoredConnection[string], error) {
			if nodeID != "node-1" || fromConnectionID != "" {
				t.Fatalf("unexpected source query: node=%q from=%q", nodeID, fromConnectionID)
			}
			return []StoredConnection[string]{{ConnectionID: "connection-1", Active: true, UpdatedAtMS: 10, Material: "material"}}, nil
		},
		func(_ context.Context, connection StoredConnection[string]) (string, error) {
			return connection.Material, nil
		},
		builder,
	)
	response := handler.BuildAuthManifestResponse(context.Background(), &commonproto.AuthManifestRequest{NodeId: "node-1", Full: true, PageSize: 10})
	if response.NodeID != "node-1" || !response.Full || !reflect.DeepEqual(response.Changes, []string{"material"}) || response.ManifestStartedAtMS == 0 {
		t.Fatalf("unexpected stored manifest response: %+v", response)
	}
}
