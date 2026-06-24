package amneziaagent

import (
	"context"
	"strings"

	commonproto "laz/generated/nodeproto/agentcommon"
	amneziaproto "laz/generated/nodeproto/amnezia"
	"laz/internal/server/agentprotocol"
	"laz/internal/server/integrations/nativeawg"
	"laz/internal/server/model"
)

type amneziaServerMessages struct{}

func (amneziaServerMessages) BuildServerMessage(parts agentprotocol.ServerMessageParts[*amneziaproto.AuthManifestResponse]) *amneziaproto.ServerMessage {
	return &amneziaproto.ServerMessage{
		Type:                 parts.Type,
		RequestId:            parts.RequestID,
		AuthRefresh:          parts.AuthRefresh,
		UsageAck:             parts.UsageAck,
		RuntimeCommand:       parts.RuntimeCommand,
		AuthManifestResponse: parts.AuthManifestResponse,
	}
}

func (amneziaServerMessages) BuildInvalidAuthManifestResponse(nodeID string, message string) *amneziaproto.AuthManifestResponse {
	return &amneziaproto.AuthManifestResponse{NodeId: nodeID, Error: message}
}

func newAmneziaManifestHandler(listConnections agentprotocol.StoredConnectionSourceFunc[model.AmneziaWGSyncMaterial]) agentprotocol.AgentServerHandler[*amneziaproto.AuthManifestResponse] {
	return agentprotocol.NewStoredManifestHandler(
		listConnections,
		buildAmneziaCredentialChange,
		amneziaproto.NewManifestResponseBuilder(),
	)
}

func buildAmneziaCredentialChange(_ context.Context, credential agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial]) (*amneziaproto.CredentialChange, error) {
	connection := credential.Material.Connection
	material := credential.Material.Material
	if !credential.Active {
		return &amneziaproto.CredentialChange{
			ConnectionId: connection.ID,
			Op:           commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_DELETE,
		}, nil
	}
	desiredStatus := connection.DesiredStatus
	if desiredStatus == "" {
		desiredStatus = model.ConnectionStatusActive
	}
	keepalive := credential.Material.NodeConfig.PersistentKeepalive
	if keepalive < 0 {
		keepalive = nativeawg.DefaultPersistentKeepalive
	}
	return &amneziaproto.CredentialChange{
		ConnectionId: connection.ID,
		Op:           commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT,
		Payload: &amneziaproto.Credential{
			PublicKey:           strings.TrimSpace(material.ClientPublicKey),
			PresharedKey:        strings.TrimSpace(material.PresharedKey),
			AssignedIpv4:        strings.TrimSpace(material.AssignedIPv4),
			DesiredStatus:       string(desiredStatus),
			PersistentKeepalive: uint32(keepalive),
		},
	}, nil
}
