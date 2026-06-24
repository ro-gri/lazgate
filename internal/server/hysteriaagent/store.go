package hysteriaagent

import (
	"context"

	"laz/internal/server/agentprotocol"
	"laz/internal/server/agenttelemetry"
	"laz/internal/server/model"
)

type Store interface {
	agenttelemetry.Store
	agentprotocol.InstallationRecordStore
	GetNode(id string) (model.Node, error)
	GetHysteria2NodeConfig(string) (model.Hysteria2NodeConfig, error)
	GetHysteriaFileSet(string) (model.HysteriaFileSet, error)
	SetHysteria2ConfigResult(string, int64, string) error
	ListHysteriaSyncConnections(ctx context.Context, nodeID, fromConnectionID string) ([]agentprotocol.StoredConnection[string], error)
	ListPendingRuntimeCommands(agentInstallationID string) []model.RuntimeCommand
	CompleteRuntimeCommandForInstallation(agentInstallationID, id string, status model.RuntimeCommandStatus, result, errMsg string) error
}
