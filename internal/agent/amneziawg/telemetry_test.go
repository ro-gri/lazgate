package amneziawg

import (
	"context"
	"laz/internal/legacy/agent/amneziawg/tools"
	"testing"
	"time"
)

type telemetryTools struct {
	calls int
	dump  tools.InterfaceDump
}

func (t *telemetryTools) ShowDump(context.Context) (tools.InterfaceDump, error) {
	t.calls++
	return t.dump, nil
}
func TestTelemetryReadsDumpOnce(t *testing.T) {
	now := time.Now()
	runtime := &telemetryTools{dump: tools.InterfaceDump{Peers: []tools.PeerDump{
		{PublicKey: "active", ReceiveBytes: 10, TransmitBytes: 20, LatestHandshake: now.Add(-time.Second)},
		{PublicKey: "stale", LatestHandshake: now.Add(-time.Hour)},
		{PublicKey: "held", AllowedIPs: []string{"0.0.0.0/32"}, LatestHandshake: now},
	}}}
	result := (telemetrySource{tools: runtime, freshness: 3 * time.Minute}).ReadTelemetry(context.Background(), now)
	if runtime.calls != 1 || len(result.Traffic) != 2 || len(result.Online) != 1 || result.Online[0].NativeAuthID != "active" {
		t.Fatalf("calls %d snapshot %+v", runtime.calls, result)
	}
}
