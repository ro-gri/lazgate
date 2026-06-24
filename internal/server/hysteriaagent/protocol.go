package hysteriaagent

import (
	"context"
	"errors"

	commonproto "laz/generated/nodeproto/agentcommon"
	hysteriaproto "laz/generated/nodeproto/hysteria"
	"laz/internal/server/agentprotocol"
)

type hysteriaServerMessages struct{}

func (hysteriaServerMessages) BuildServerMessage(parts agentprotocol.ServerMessageParts[*hysteriaproto.AuthManifestResponse]) *hysteriaproto.ServerMessage {
	return &hysteriaproto.ServerMessage{
		Type:                 parts.Type,
		RequestId:            parts.RequestID,
		AuthRefresh:          parts.AuthRefresh,
		UsageAck:             parts.UsageAck,
		RuntimeCommand:       parts.RuntimeCommand,
		AuthManifestResponse: parts.AuthManifestResponse,
	}
}

func (hysteriaServerMessages) BuildInvalidAuthManifestResponse(nodeID string, message string) *hysteriaproto.AuthManifestResponse {
	return &hysteriaproto.AuthManifestResponse{NodeId: nodeID, Error: message}
}

func newHysteriaManifestHandler(listConnections agentprotocol.StoredConnectionSourceFunc[string]) agentprotocol.AgentServerHandler[*hysteriaproto.AuthManifestResponse] {
	return agentprotocol.NewStoredManifestHandler(
		listConnections,
		buildHysteriaCredentialChange,
		hysteriaproto.NewManifestResponseBuilder(),
	)
}

func buildHysteriaCredentialChange(_ context.Context, credential agentprotocol.StoredConnection[string]) (*hysteriaproto.CredentialChange, error) {
	change := &hysteriaproto.CredentialChange{
		ConnectionId: credential.ConnectionID,
		Op:           commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_DELETE,
	}
	if !credential.Active {
		return change, nil
	}
	passwordHash := credential.Material
	if passwordHash == "" {
		return nil, errors.New("active Hysteria credential has no password hash")
	}
	change.Op = commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT
	change.Payload = &hysteriaproto.Credential{PasswordHash: passwordHash}
	return change, nil
}
