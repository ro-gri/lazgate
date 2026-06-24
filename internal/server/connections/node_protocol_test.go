package connections

import (
	"testing"

	"laz/internal/server/model"
)

func TestEnrollmentProtocolForNodeIncludesNativeAmneziaWG(t *testing.T) {
	tests := []struct {
		name string
		node model.Node
		want model.Protocol
	}{
		{name: "external amnezia api unchanged", node: model.Node{Type: model.NodeTypeAmneziaAPI}, want: model.ProtocolAmneziaWG},
		{name: "native amneziawg", node: model.Node{Type: model.NodeTypeNativeAmneziaWG}, want: model.ProtocolAmneziaWG},
		{name: "native hysteria unchanged", node: model.Node{Type: model.NodeTypeNativeHy2}, want: model.ProtocolHysteria2},
		{name: "unknown", node: model.Node{Type: model.NodeType("unknown")}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnrollmentProtocolForNode(tt.node); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
