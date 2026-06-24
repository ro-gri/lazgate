package amneziaagent

import (
	"context"
	"testing"

	commonproto "laz/generated/nodeproto/agentcommon"
	"laz/internal/server/agentprotocol"
	"laz/internal/server/integrations/nativeawg"
	"laz/internal/server/model"
)

func TestBuildAmneziaCredentialChange(t *testing.T) {
	material := model.AmneziaWGSyncMaterial{
		NodeConfig: model.AmneziaWGNodeConfig{PersistentKeepalive: nativeawg.DefaultPersistentKeepalive},
		Connection: model.Connection{ID: "connection-1", Name: "Phone", DesiredStatus: model.ConnectionStatusActive, DesiredVersionMS: 123},
		Material: model.AmneziaWGConnectionMaterial{
			ClientPublicKey: " public-key ", PresharedKey: " preshared-key ", AssignedIPv4: "10.8.1.2",
		},
	}
	upsert, err := buildAmneziaCredentialChange(context.Background(), agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial]{Active: true, Material: material})
	if err != nil {
		t.Fatal(err)
	}
	payload := upsert.GetPayload()
	if upsert.GetConnectionId() != "connection-1" || upsert.GetOp() != commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT || payload.GetPublicKey() != "public-key" || payload.GetPresharedKey() != "preshared-key" || payload.GetAssignedIpv4() != "10.8.1.2" || payload.GetPersistentKeepalive() != nativeawg.DefaultPersistentKeepalive {
		t.Fatalf("unexpected upsert change: %+v", upsert)
	}

	deleted, err := buildAmneziaCredentialChange(context.Background(), agentprotocol.StoredConnection[model.AmneziaWGSyncMaterial]{Material: material})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.GetConnectionId() != "connection-1" || deleted.GetOp() != commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_DELETE || deleted.GetPayload() != nil {
		t.Fatalf("unexpected delete change: %+v", deleted)
	}
}
