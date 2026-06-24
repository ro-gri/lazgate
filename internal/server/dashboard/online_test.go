package dashboard

import (
	"laz/internal/server/model"
	"testing"
	"time"
)

func TestOnlineRowsKeepUnknownNativeIdentitiesSeparate(t *testing.T) {
	rows := onlineUserRows(map[string][]model.NodeOnlineConnection{
		"unknown:one": {{NodeInstanceID: "instance", NativeAuthID: "one", Count: 1, LastSeenAt: time.Now()}},
		"unknown:two": {{NodeInstanceID: "instance", NativeAuthID: "two", Count: 2, LastSeenAt: time.Now()}},
	}, nil, nil, map[string]string{"instance": "node"}, nil, 100)
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	for _, row := range rows {
		if row.ConnectionID != "" || len(row.NativeAuthIDs) != 1 || row.DisplayName != "Неизвестное подключение" {
			t.Fatalf("row %+v", row)
		}
	}
}
