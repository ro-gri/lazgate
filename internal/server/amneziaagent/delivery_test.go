package amneziaagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	commonproto "laz/generated/nodeproto/agentcommon"
	amneziaproto "laz/generated/nodeproto/amnezia"
	"laz/internal/queue"
	"laz/internal/server/agentprotocol"
	"laz/internal/server/model"
)

type marshalFailureQueue struct {
	queue.NopStore
	failed string
	acked  bool
}

func (s *marshalFailureQueue) MarkFailed(_ context.Context, _ string, err string, _ time.Time) error {
	s.failed = err
	return nil
}
func (s *marshalFailureQueue) MarkAcked(context.Context, string, []byte) error {
	s.acked = true
	return nil
}

type deliveryTestRequester struct{ response *commonproto.AgentMessage }
type deliveryConfigStore struct{ Store }

func (deliveryConfigStore) GetAmneziaWGNodeConfig(string) (model.AmneziaWGNodeConfig, error) {
	return model.AmneziaWGNodeConfig{ConfigVersionMS: 1}, nil
}

func (r deliveryTestRequester) Request(context.Context, string, string, *amneziaproto.ServerMessage) (*commonproto.AgentMessage, error) {
	return r.response, nil
}

func TestDeliveryMarshalFailureDoesNotPanicOrAcknowledge(t *testing.T) {
	for _, typ := range []string{"auth_refresh", "edit_server_config"} {
		t.Run(typ, func(t *testing.T) {
			ctx := context.Background()
			node := agentprotocol.NewStreamNode[*commonproto.AgentMessage, *amneziaproto.ServerMessage]("node", "installation", func() {}, 1, 1)
			var command proto.Message = &commonproto.AuthRefresh{NodeId: "node"}
			response := &commonproto.AgentMessage{AuthRefreshResult: &commonproto.AuthRefreshResult{Status: "applied", Error: string([]byte{0xff})}}
			if typ == "edit_server_config" {
				command = &amneziaproto.EditServerConfig{NodeId: "node", Revision: 1}
				response = &commonproto.AgentMessage{ServerConfigApplyResult: &commonproto.ServerConfigApplyResult{NodeId: "node", DesiredRevision: 1, AppliedRevision: 1, CorrelationId: "request", Status: "applied", Error: string([]byte{0xff})}}
			}
			payload, err := proto.Marshal(command)
			if err != nil {
				t.Fatal(err)
			}
			store := &marshalFailureQueue{}
			service := &deliveryService{store: deliveryConfigStore{}, transport: store, requester: deliveryTestRequester{response: response}}
			message := queue.Message{ID: "request", Payload: payload}
			if typ == "auth_refresh" {
				service.dispatchAuthRefresh(ctx, node, message)
			} else {
				service.dispatchServerConfig(ctx, node, message)
			}
			if store.acked || !strings.Contains(store.failed, "marshal") {
				t.Fatalf("acked=%v failed=%q", store.acked, store.failed)
			}
		})
	}
}
