package amneziawg

import (
	"context"
	agentcommon "laz/internal/agent/common"
	"laz/internal/legacy/agent/amneziawg/tools"
	"math"
	"strings"
	"time"
)

type telemetrySource struct {
	tools interface {
		ShowDump(context.Context) (tools.InterfaceDump, error)
	}
	freshness time.Duration
}

func (s telemetrySource) ReadTelemetry(ctx context.Context, now time.Time) agentcommon.NativeTelemetry {
	dump, err := s.tools.ShowDump(ctx)
	result := agentcommon.NativeTelemetry{TrafficError: err, OnlineError: err}
	if err != nil {
		return result
	}
	for _, p := range dump.Peers {
		blocked := false
		for _, ip := range p.AllowedIPs {
			if strings.TrimSpace(ip) == "0.0.0.0/32" {
				blocked = true
				break
			}
		}
		id := strings.TrimSpace(p.PublicKey)
		if blocked || id == "" {
			continue
		}
		result.Traffic = append(result.Traffic, agentcommon.NativeTrafficRecord{NativeAuthID: id, RXBytes: int64(min(p.ReceiveBytes, uint64(math.MaxInt64))), TXBytes: int64(min(p.TransmitBytes, uint64(math.MaxInt64)))})
		if !p.LatestHandshake.IsZero() && !p.LatestHandshake.After(now) && s.freshness > 0 && now.Sub(p.LatestHandshake) <= s.freshness {
			result.Online = append(result.Online, agentcommon.NativeOnlineRecord{NativeAuthID: id, Count: 1})
		}
	}
	return result
}
