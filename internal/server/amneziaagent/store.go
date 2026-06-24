package amneziaagent

import (
	"context"

	"laz/internal/server/agentprotocol"
	"laz/internal/server/agenttelemetry"
	"laz/internal/server/model"
)

type Store interface {
	agenttelemetry.Store
	agentprotocol.InstallationRecordStore
	ListAmneziaWGSyncConnections(ctx context.Context, nodeID, fromConnectionID string) ([]agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial], error)
	GetAmneziaWGNodeConfig(nodeID string) (model.AmneziaWGNodeConfig, error)
	SetAmneziaWGConfigResult(string, int64, string) error
	ListPendingRuntimeCommands(agentInstallationID string) []model.RuntimeCommand
	CompleteRuntimeCommandForInstallation(agentInstallationID, id string, status model.RuntimeCommandStatus, result, errMsg string) error
}
