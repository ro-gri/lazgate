package agentcontrol

import (
	"errors"
	"math"
	"time"

	"laz/internal/nodeproto"
	"laz/internal/server/model"
)

type Store interface {
	UpsertNodeRuntime(model.NodeRuntime) error
	UpsertNodeOnlineClients(nodeID string, clients []model.NodeOnlineClient) error
	CreateUsageBatch(model.UsageBatch, []model.UsageRecord) (bool, error)
	GetNode(id string) (model.Node, error)
	ListConnections() []model.Connection
	ListIssuedConfigs() []model.IssuedConfig
	GetAccount(id string) (model.Account, error)
	GetClientForAccount(accountID, clientID string) (model.Client, error)
	ListPendingRuntimeCommands(nodeID string) []model.RuntimeCommand
	CompleteRuntimeCommand(id string, status model.Status, result, errMsg string) error
}

func runtimeFromHeartbeat(input *nodeproto.Heartbeat) model.NodeRuntime {
	now := time.Now().UTC()
	if input == nil {
		return model.NodeRuntime{}
	}
	return model.NodeRuntime{
		NodeID:                     input.GetNodeId(),
		AgentStatus:                "online",
		LastHeartbeatAt:            now,
		AgentVersion:               input.GetAgentVersion(),
		ProtocolVersion:            input.GetProtocolVersion(),
		HysteriaServiceStatus:      input.GetHysteriaServiceStatus(),
		LastTrafficCollectionAt:    msTime(input.GetLastTrafficCollectionMs()),
		LastOnlineCollectionAt:     msTime(input.GetLastOnlineCollectionMs()),
		PendingUsageBatchCount:     int(input.GetPendingUsageBatchCount()),
		PendingUsageQueueSizeBytes: input.GetPendingUsageQueueSizeBytes(),
	}
}

func onlineFromReport(input *nodeproto.OnlineReport) []model.NodeOnlineClient {
	if input == nil {
		return nil
	}
	var clients []model.NodeOnlineClient
	for _, item := range input.GetClients() {
		clients = append(clients, model.NodeOnlineClient{
			NodeID:       input.GetNodeId(),
			CredentialID: item.GetCredentialId(),
			Count:        int(item.GetCount()),
			LastSeenAt:   msTime(input.GetAtMs()),
		})
	}
	return clients
}

func usageFromProto(input *nodeproto.UsageBatch, streamNodeID string) (model.UsageBatch, []model.UsageRecord, error) {
	if input == nil {
		return model.UsageBatch{}, nil, errors.New("empty usage batch")
	}
	if input.GetNodeId() != streamNodeID {
		return model.UsageBatch{}, nil, errors.New("usage batch node mismatch")
	}
	var records []model.UsageRecord
	for _, rec := range input.GetRecords() {
		if rec.GetTxBytes() < 0 || rec.GetRxBytes() < 0 {
			return model.UsageBatch{}, nil, errors.New("negative usage values")
		}
		if rec.GetTxBytes() > math.MaxInt64-rec.GetRxBytes() {
			return model.UsageBatch{}, nil, errors.New("usage values overflow")
		}
		records = append(records, model.UsageRecord{CredentialID: rec.GetCredentialId(), TXBytes: rec.GetTxBytes(), RXBytes: rec.GetRxBytes()})
	}
	return model.UsageBatch{BatchID: input.GetBatchId(), NodeID: input.GetNodeId(), FromMS: input.GetFromMs(), ToMS: input.GetToMs()}, records, nil
}
