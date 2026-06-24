package common

import "log/slog"

// Identity is the immutable runtime identity shared by all agent components.
type Identity struct {
	NodeID         string
	InstallationID string
	AgentType      string
}

func (i Identity) Logger() *slog.Logger {
	return slog.Default().With("node_id", i.NodeID, "agent_type", i.AgentType)
}
