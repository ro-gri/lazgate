package hysteria2

import (
	"context"
	agentcommon "laz/internal/agent/common"
	"laz/internal/legacy/agent/hysteria2/statsapi"
	"time"
)

type telemetrySource struct{ stats *statsapi.Client }

func (s telemetrySource) ReadTelemetry(ctx context.Context, _ time.Time) agentcommon.NativeTelemetry {
	var result agentcommon.NativeTelemetry
	traffic, err := s.stats.Traffic(ctx)
	result.TrafficError = err
	for _, r := range traffic {
		result.Traffic = append(result.Traffic, agentcommon.NativeTrafficRecord{NativeAuthID: r.NativeAuthID, RXBytes: r.RXBytes, TXBytes: r.TXBytes})
	}
	online, err := s.stats.Online(ctx)
	result.OnlineError = err
	for _, r := range online {
		result.Online = append(result.Online, agentcommon.NativeOnlineRecord{NativeAuthID: r.NativeAuthID, Count: r.Count})
	}
	return result
}
