package subscriptions

import (
	"strings"
	"testing"

	"laz/internal/server/model"
)

func TestSubscriptionContainsExactlyTokenConnection(t *testing.T) {
	node := model.Node{ID: "node-1", Name: "Amsterdam", Region: "NL-Amsterdam"}
	first := model.Connection{ID: "connection-1", AccountID: "account-1", NodeID: node.ID, Name: "Phone", Protocol: model.ProtocolHysteria2, DesiredStatus: model.ConnectionStatusActive}
	second := model.Connection{ID: "connection-2", AccountID: "account-1", NodeID: node.ID, Name: "Laptop", Protocol: model.ProtocolHysteria2, DesiredStatus: model.ConnectionStatusActive}
	summary := model.AccountSummary{
		Account: model.Account{ID: "account-1"},
		Connections: []model.ConnectionSummary{
			{Connection: first, Node: node},
			{Connection: second, Node: node},
		},
		Configs: []model.IssuedConfig{
			{ConnectionID: first.ID, Kind: model.ConfigHy2URI, Name: "Phone", Config: "hysteria2://first@example:443", Status: model.StatusActive},
			{ConnectionID: second.ID, Kind: model.ConfigHy2URI, Name: "Laptop", Config: "hysteria2://second@example:443", Status: model.StatusActive},
		},
	}

	body := New(nil).HPSubscriptionBody(model.ConnectionSubscriptionToken{ConnectionID: first.ID}, summary)
	if !strings.Contains(body, "hysteria2://first@example:443") {
		t.Fatalf("selected connection missing from subscription: %q", body)
	}
	if strings.Contains(body, "second@example") {
		t.Fatalf("subscription leaked another connection: %q", body)
	}
}
