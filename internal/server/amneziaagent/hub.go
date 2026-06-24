package amneziaagent

import (
	commonproto "laz/generated/nodeproto/agentcommon"
	amneziaproto "laz/generated/nodeproto/amnezia"
	transportstore "laz/internal/queue"
	"laz/internal/server/agentprotocol"
	"laz/internal/server/agenttelemetry"
	"laz/internal/server/model"

	"google.golang.org/grpc"
)

type Hub struct {
	amneziaproto.UnimplementedAgentServer
	*agentprotocol.AgentServer[amneziaproto.ServerMessage, *amneziaproto.AuthManifestResponse]
	*deliveryService
}

type streamNode = agentprotocol.StreamNode[*commonproto.AgentMessage, *amneziaproto.ServerMessage]

func NewHub(st Store, expectedAgentType string, stores ...transportstore.Store) *Hub {
	var manifestSource agentprotocol.StoredConnectionSourceFunc[model.AmneziaWGSyncMaterial]
	if st != nil {
		manifestSource = st.ListAmneziaWGSyncConnections
	}
	transport := agentprotocol.SelectTransportStore(stores)
	delivery := newDeliveryService(st, transport)
	telemetry := agenttelemetry.NewAdapter[amneziaproto.ServerMessage](st, transport, nil)
	h := &Hub{deliveryService: delivery}
	h.AgentServer = agentprotocol.NewAgentServer[amneziaproto.ServerMessage, *amneziaproto.AuthManifestResponse](
		newAmneziaManifestHandler(manifestSource),
		delivery,
		amneziaServerMessages{},
		telemetry,
		expectedAgentType,
		agentprotocol.NewInstallationLookup(st),
	)
	delivery.requester = h.AgentServer
	return h
}

func (h *Hub) Connect(stream grpc.BidiStreamingServer[commonproto.AgentMessage, amneziaproto.ServerMessage]) error {
	return h.AgentServer.Connect(stream)
}
