package traffic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"laz/internal/agent/hysteria2/statsapi"
	agentstore "laz/internal/agent/store"
)

type Store interface {
	SaveUsageBatch(context.Context, agentstore.UsageBatch) error
}

type Collector struct {
	nodeID string
	stats  *statsapi.Client
	store  Store
}

func New(nodeID string, stats *statsapi.Client, st Store) *Collector {
	return &Collector{nodeID: nodeID, stats: stats, store: st}
}

func (c *Collector) Collect(ctx context.Context) (agentstore.UsageBatch, error) {
	to := time.Now()
	from := to.Add(-time.Minute)
	records, err := c.stats.Traffic(ctx, true)
	if err != nil {
		return agentstore.UsageBatch{}, err
	}
	batch := agentstore.UsageBatch{
		BatchID:   "batch_" + randomHex(),
		NodeID:    c.nodeID,
		FromMS:    from.UnixMilli(),
		ToMS:      to.UnixMilli(),
		CreatedMS: to.UnixMilli(),
	}
	for _, rec := range records {
		if rec.CredentialID == "" || (rec.TXBytes == 0 && rec.RXBytes == 0) {
			continue
		}
		batch.Records = append(batch.Records, agentstore.UsageRecord{CredentialID: rec.CredentialID, TXBytes: rec.TXBytes, RXBytes: rec.RXBytes})
	}
	if len(batch.Records) == 0 {
		return batch, nil
	}
	return batch, c.store.SaveUsageBatch(ctx, batch)
}

func randomHex() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
