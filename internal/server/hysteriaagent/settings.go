package hysteriaagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	commonproto "laz/generated/nodeproto/agentcommon"
	hysteriaproto "laz/generated/nodeproto/hysteria"
	"laz/internal/queue"
	"laz/internal/server/agentcommands"
	"laz/internal/server/hysteriasettings"
)

type configRequester interface {
	Request(context.Context, string, string, *hysteriaproto.ServerMessage) (*commonproto.AgentMessage, error)
}
type settingsDelivery struct {
	store     Store
	transport queue.Store
	requester configRequester
	*agentcommands.Service[hysteriaproto.ServerMessage]
}

func (d *settingsDelivery) RunNodeTasks(ctx context.Context, node *streamNode) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var queued int64
	for {
		d.Dispatch(node)
		c, err := d.store.GetHysteria2NodeConfig(node.NodeID())
		if err == nil && c.SettingsJSON != "" && c.ConfigVersionMS != queued {
			// Persist only the revision. Secrets are loaded from encrypted domain storage when sent.
			raw, _ := json.Marshal(c.ConfigVersionMS)
			err = d.transport.Enqueue(ctx, queue.Message{ID: uuid.NewString(), ActorID: "h2-settings/" + node.NodeID(), Type: "edit_server_config", Payload: raw, Status: queue.StatusPending, ExpiresAt: time.Now().Add(24 * time.Hour)})
			if err != nil {
				return err
			}
			queued = c.ConfigVersionMS
		}
		actor := "h2-settings/" + node.NodeID()
		if err := d.transport.RequeueExpiredLeases(ctx, actor); err != nil {
			return err
		}
		messages, err := d.transport.LeasePending(ctx, actor, 1, 2*time.Minute)
		if err != nil {
			return err
		}
		for _, m := range messages {
			d.apply(ctx, node, m)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-node.Done():
			return nil
		case <-ticker.C:
		}
	}
}
func (d *settingsDelivery) apply(ctx context.Context, node *streamNode, m queue.Message) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	retry := func(err error) { _ = d.transport.MarkFailed(ctx, m.ID, err.Error(), time.Now().Add(5*time.Second)) }
	var version int64
	if err := json.Unmarshal(m.Payload, &version); err != nil {
		_ = d.transport.MarkFailed(ctx, m.ID, "invalid configuration revision", time.Time{})
		return
	}
	c, err := d.store.GetHysteria2NodeConfig(node.NodeID())
	if err != nil {
		retry(err)
		return
	}
	if version != c.ConfigVersionMS {
		_ = d.transport.MarkExpired(ctx, m.ID, "superseded configuration")
		return
	}
	settings, err := hysteriasettings.Settings(c)
	if err != nil {
		retry(err)
		return
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		retry(err)
		return
	}
	command := &hysteriaproto.EditServerConfig{NodeId: node.NodeID(), Revision: uint64(version), ServerConfig: &hysteriaproto.ServerConfig{EndpointHost: c.EndpointHost, SettingsJson: raw}}
	if settings.Masquerade.Type == "file" && c.MasqueradeFileSetID != "" {
		bundle, err := d.store.GetHysteriaFileSet(c.MasqueradeFileSetID)
		if err != nil {
			retry(err)
			return
		}
		archive, err := base64.StdEncoding.DecodeString(bundle.Archive)
		if err != nil {
			retry(err)
			return
		}
		command.ServerConfig.MasqueradeFilesetId = bundle.ID
		command.ServerConfig.MasqueradeArchive = archive
	}
	response, err := d.requester.Request(ctx, node.NodeID(), m.ID, &hysteriaproto.ServerMessage{Type: "edit_server_config", RequestId: m.ID, EditServerConfig: command})
	if err != nil {
		retry(err)
		return
	}
	result := response.GetServerConfigApplyResult()
	if result == nil || result.GetNodeId() != node.NodeID() || result.GetDesiredRevision() != uint64(version) || result.GetCorrelationId() != m.ID {
		retry(fmt.Errorf("invalid Hysteria configuration acknowledgement"))
		return
	}
	if (result.GetStatus() == "ok" || result.GetStatus() == "applied") && result.GetAppliedRevision() == uint64(version) {
		if err = d.store.SetHysteria2ConfigResult(node.NodeID(), version, ""); err != nil {
			retry(err)
			return
		}
		raw, _ := proto.Marshal(result)
		_ = d.transport.MarkAcked(ctx, m.ID, raw)
		return
	}
	id := uuid.NewString()
	slog.Error("Hysteria settings apply failed", "error_id", id, "node_id", node.NodeID(), "revision", version, "error", result.GetError())
	_ = d.store.SetHysteria2ConfigResult(node.NodeID(), version, id)
	_ = d.transport.MarkFailed(ctx, m.ID, id, time.Time{})
}
