package common

import (
	"context"
	"errors"
	"time"

	commonproto "laz/generated/nodeproto/agentcommon"
	agentstore "laz/internal/agent/common/store"
)

type NativeTrafficRecord struct {
	NativeAuthID     string
	RXBytes, TXBytes int64
}
type NativeOnlineRecord struct {
	NativeAuthID string
	Count        int
}
type NativeTelemetry struct {
	Traffic      []NativeTrafficRecord
	Online       []NativeOnlineRecord
	TrafficError error
	OnlineError  error
}
type TelemetrySource interface {
	ReadTelemetry(context.Context, time.Time) NativeTelemetry
}
type TelemetrySnapshot struct {
	Traffic      agentstore.TrafficSnapshot
	Online       *commonproto.OnlineReport
	TrafficError error
	OnlineError  error
}
type TelemetryCollector struct {
	source TelemetrySource
	lookup AuthLookup
}

func NewTelemetryCollector(source TelemetrySource, lookup AuthLookup) *TelemetryCollector {
	return &TelemetryCollector{source: source, lookup: lookup}
}
func (c *TelemetryCollector) Collect(ctx context.Context) TelemetrySnapshot {
	now := time.Now().UTC()
	native := c.source.ReadTelemetry(ctx, now)
	result := TelemetrySnapshot{Traffic: agentstore.TrafficSnapshot{CollectedMS: now.UnixMilli()}, TrafficError: native.TrafficError, OnlineError: native.OnlineError}
	ids := map[string]string{}
	failures := map[string]error{}
	resolve := func(id string) (string, error) {
		if v, ok := ids[id]; ok {
			return v, failures[id]
		}
		v, err := c.lookup.GetConnectionIDByNativeAuthID(ctx, id)
		if errors.Is(err, agentstore.ErrNotFound) {
			err = nil
			v = ""
		}
		ids[id] = v
		failures[id] = err
		return v, err
	}
	if result.TrafficError == nil {
		for _, r := range native.Traffic {
			id, err := resolve(r.NativeAuthID)
			if err != nil {
				result.TrafficError = err
				break
			}
			if id != "" {
				result.Traffic.Records = append(result.Traffic.Records, agentstore.UsageRecord{ConnectionID: id, RXBytes: r.RXBytes, TXBytes: r.TXBytes})
			}
		}
	}
	if result.OnlineError == nil {
		result.Online = &commonproto.OnlineReport{AtMs: now.UnixMilli()}
		for _, r := range native.Online {
			if r.NativeAuthID == "" || r.Count <= 0 {
				continue
			}
			id, err := resolve(r.NativeAuthID)
			if err != nil {
				result.OnlineError = err
				result.Online = nil
				break
			}
			result.Online.Connections = append(result.Online.Connections, &commonproto.OnlineConnection{ConnectionId: id, NativeAuthId: r.NativeAuthID, Count: int32(r.Count)})
		}
	}
	return result
}
