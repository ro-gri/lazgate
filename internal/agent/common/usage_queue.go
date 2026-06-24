package common

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	agentstore "laz/internal/agent/common/store"
	transportstore "laz/internal/queue"

	"github.com/google/uuid"
)

const usageRecordsPerMessage = 100

type usageQueue struct {
	identity  Identity
	state     StateStore
	transport transportstore.Store
}

func newUsageQueue(identity Identity, state StateStore, transport transportstore.Store) *usageQueue {
	return &usageQueue{identity: identity, state: state, transport: transport}
}

func (q *usageQueue) Observe(ctx context.Context, snapshot agentstore.TrafficSnapshot) error {
	return q.state.ObserveTraffic(ctx, snapshot)
}

// EnsureOutbox repairs the intentional cross-database boundary: traffic rows
// are frozen first, then a durable outbox message is inserted. Repeating this
// operation after a crash is safe because the message ID is stable.
func (q *usageQueue) EnsureOutbox(ctx context.Context) error {
	prepared, err := q.state.ListPreparedUsageRequests(ctx, q.identity.NodeID)
	if err != nil {
		return err
	}
	for _, batch := range prepared {
		if err := q.enqueue(ctx, batch); err != nil {
			return err
		}
	}
	for {
		batch, ok, err := q.state.PrepareUsageRequest(ctx, uuid.NewString(), q.identity.NodeID, usageRecordsPerMessage)
		if err != nil || !ok {
			return err
		}
		if err := q.enqueue(ctx, batch); err != nil {
			return err
		}
	}
}

func (q *usageQueue) enqueue(ctx context.Context, batch agentstore.UsageBatch) error {
	if batch.RequestID == "" || len(batch.Records) == 0 {
		return errors.New("prepared traffic request is invalid")
	}
	now := time.Now().UTC()
	if batch.CreatedMS == 0 {
		batch.CreatedMS = now.UnixMilli()
	}
	if batch.ToMS == 0 {
		batch.ToMS = now.UnixMilli()
	}
	if batch.FromMS == 0 {
		batch.FromMS = batch.ToMS
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	return q.transport.EnqueueIfAbsent(ctx, transportstore.Message{
		ID:          batch.RequestID,
		ActorID:     q.identity.NodeID,
		Direction:   transportstore.DirectionOutbound,
		Type:        "traffic_batch",
		Payload:     raw,
		Status:      transportstore.StatusPending,
		AvailableAt: now,
	})
}

func (q *usageQueue) List(ctx context.Context, limit int) ([]transportstore.Message, error) {
	if err := q.EnsureOutbox(ctx); err != nil {
		return nil, err
	}
	return q.transport.LeasePending(ctx, q.identity.NodeID, limit, time.Minute)
}

func (q *usageQueue) Ack(ctx context.Context, requestID string, ack *commonproto.UsageAck) error {
	if ack == nil || !ack.GetOk() {
		return errors.New("usage acknowledgement was rejected")
	}
	records := make([]agentstore.AcknowledgedUsage, 0, len(ack.GetRecords()))
	for _, record := range ack.GetRecords() {
		records = append(records, agentstore.AcknowledgedUsage{
			ConnectionID: record.GetConnectionId(),
			RXBytes:      record.GetRxBytes(),
			TXBytes:      record.GetTxBytes(),
		})
	}
	if err := q.state.AcknowledgeUsage(ctx, requestID, records); err != nil {
		return err
	}
	raw, err := json.Marshal(ack)
	if err != nil {
		return err
	}
	return q.transport.MarkAcked(ctx, requestID, raw)
}

func (q *usageQueue) Fail(ctx context.Context, requestID string, cause error) {
	errMsg := ""
	if cause != nil {
		errMsg = cause.Error()
	}
	_ = q.transport.MarkFailed(ctx, requestID, errMsg, time.Now().UTC().Add(time.Minute))
}

func decodeUsageBatch(msg transportstore.Message) (agentstore.UsageBatch, error) {
	var batch agentstore.UsageBatch
	err := json.Unmarshal(msg.Payload, &batch)
	if err == nil && batch.RequestID != msg.ID {
		return agentstore.UsageBatch{}, errors.New("traffic payload request_id does not match outbox id")
	}
	return batch, err
}
