package hysteriaagent

import (
	commonproto "laz/generated/nodeproto/agentcommon"
	hysteriaproto "laz/generated/nodeproto/hysteria"
	transportstore "laz/internal/queue"
	"laz/internal/server/agentcommands"
	"laz/internal/server/agentprotocol"
	"laz/internal/server/agenttelemetry"
	"laz/internal/server/model"

	"google.golang.org/grpc"
)

type Hub struct {
	hysteriaproto.UnimplementedAgentServer
	*agentprotocol.AgentServer[hysteriaproto.ServerMessage, *hysteriaproto.AuthManifestResponse]
}

type streamNode = agentprotocol.StreamNode[*commonproto.AgentMessage, *hysteriaproto.ServerMessage]

func NewHub(st Store, expectedAgentType string, stores ...transportstore.Store) *Hub {
	var manifestSource agentprotocol.StoredConnectionSourceFunc[string]
	if st != nil {
		manifestSource = st.ListHysteriaSyncConnections
	}
	transport := agentprotocol.SelectTransportStore(stores)
	telemetry := agenttelemetry.NewAdapter[hysteriaproto.ServerMessage](st, transport, func(nodeID string) bool {
		node, err := st.GetNode(nodeID)
		return err != nil || node.Type != model.NodeTypeNativeAmneziaWG
	})
	commands := agentcommands.New(st, func(requestID string, command *commonproto.RuntimeCommand) *hysteriaproto.ServerMessage {
		return hysteriaServerMessages{}.BuildServerMessage(agentprotocol.ServerMessageParts[*hysteriaproto.AuthManifestResponse]{
			RequestID: requestID, Type: "runtime_command", RuntimeCommand: command,
		})
	})
	h := &Hub{}
	delivery := &settingsDelivery{store: st, transport: transport, Service: commands}
	h.AgentServer = agentprotocol.NewAgentServer[hysteriaproto.ServerMessage, *hysteriaproto.AuthManifestResponse](
		newHysteriaManifestHandler(manifestSource),
		delivery,
		hysteriaServerMessages{},
		telemetry,
		expectedAgentType,
		agentprotocol.NewInstallationLookup(st),
	)
	delivery.requester = h.AgentServer
	return h
}

func (h *Hub) Connect(stream grpc.BidiStreamingServer[commonproto.AgentMessage, hysteriaproto.ServerMessage]) error {
	return h.AgentServer.Connect(stream)
}
