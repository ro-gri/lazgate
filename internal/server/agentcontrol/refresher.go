package agentcontrol

import "context"

func (h *Hub) RefreshUserAuthApplied(ctx context.Context, nodeID string, accountID string, snapshotVersionMS int64) (int64, error) {
	result, err := h.RefreshUserAuth(ctx, nodeID, accountID, snapshotVersionMS)
	if err != nil {
		return 0, err
	}
	applied := result.GetAppliedSnapshotVersionMs()
	if applied == 0 {
		applied = snapshotVersionMS
	}
	return applied, nil
}
