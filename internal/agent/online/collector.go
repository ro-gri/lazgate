package online

import (
	"context"
	"time"

	"laz/internal/agent/hysteria2/statsapi"
)

type Report struct {
	NodeID  string         `json:"node_id"`
	AtMS    int64          `json:"at_ms"`
	Clients []OnlineClient `json:"clients"`
}

type OnlineClient struct {
	CredentialID string `json:"credential_id"`
	Count        int    `json:"count"`
}

type Collector struct {
	nodeID string
	stats  *statsapi.Client
}

func New(nodeID string, stats *statsapi.Client) *Collector {
	return &Collector{nodeID: nodeID, stats: stats}
}

func (c *Collector) Collect(ctx context.Context) (Report, error) {
	records, err := c.stats.Online(ctx)
	if err != nil {
		return Report{}, err
	}
	report := Report{NodeID: c.nodeID, AtMS: time.Now().UnixMilli()}
	for _, rec := range records {
		report.Clients = append(report.Clients, OnlineClient{CredentialID: rec.CredentialID, Count: rec.Count})
	}
	return report, nil
}
