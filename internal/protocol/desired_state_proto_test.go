package protocol_test

import (
	commonproto "laz/generated/nodeproto/agentcommon"
	"reflect"
	"testing"

	amneziaproto "laz/generated/nodeproto/amnezia"
	hysteriaproto "laz/generated/nodeproto/hysteria"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestGeneratedProtoOmitsLegacyAgentIdentityFields(t *testing.T) {
	t.Parallel()

	checks := []struct {
		message proto.Message
		field   protoreflect.Name
		goType  reflect.Type
		members []string
	}{
		{message: &hysteriaproto.Credential{}, field: "user_id", goType: reflect.TypeOf(hysteriaproto.Credential{}), members: []string{"UserId", "GetUserId"}},
		{message: &hysteriaproto.Credential{}, field: "username", goType: reflect.TypeOf(hysteriaproto.Credential{}), members: []string{"Username", "GetUsername"}},
		{message: &amneziaproto.Credential{}, field: "connection_id", goType: reflect.TypeOf(amneziaproto.Credential{}), members: []string{"ConnectionId", "GetConnectionId"}},
	}
	for _, check := range checks {
		desc := check.message.ProtoReflect().Descriptor()
		if desc.Fields().ByName(check.field) != nil {
			t.Fatalf("generated %s still exposes removed descriptor field %s", desc.FullName(), check.field)
		}
		for _, member := range check.members {
			if _, ok := check.goType.FieldByName(member); ok {
				t.Fatalf("generated %s still exposes removed Go field %s", desc.FullName(), member)
			}
			if _, ok := reflect.PointerTo(check.goType).MethodByName(member); ok {
				t.Fatalf("generated %s still exposes removed Go method %s", desc.FullName(), member)
			}
		}
	}
}

func TestAmneziaAuthManifestRoundTrip(t *testing.T) {
	t.Parallel()

	in := &amneziaproto.ServerMessage{
		Type:      "auth_manifest_response",
		RequestId: "req-1",
		AuthManifestResponse: &amneziaproto.AuthManifestResponse{
			NodeId:            "node-1",
			ManifestStartedAt: 42,
			Changes: []*amneziaproto.CredentialChange{{
				Op:           commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT,
				ConnectionId: "cred-1",
				Payload: &amneziaproto.Credential{
					PublicKey:           "peer-public",
					PresharedKey:        "psk-secret",
					AssignedIpv4:        "10.8.1.2",
					DesiredStatus:       "active",
					PersistentKeepalive: 25,
				},
			}},
		},
	}

	data, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal change list: %v", err)
	}

	var out amneziaproto.ServerMessage
	if err := proto.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal change list: %v", err)
	}
	changes := out.GetAuthManifestResponse()
	if changes.GetManifestStartedAt() != 42 {
		t.Fatalf("manifest_started_at = %d", changes.GetManifestStartedAt())
	}
	peer := changes.GetChanges()[0]
	if peer.GetConnectionId() != "cred-1" || peer.GetPayload().GetPublicKey() != "peer-public" || peer.GetPayload().GetAssignedIpv4() != "10.8.1.2" {
		t.Fatalf("peer change = %#v", peer)
	}
}

func TestAmneziaCredentialDoesNotCarryClientPrivateKey(t *testing.T) {
	t.Parallel()

	peerFields := (&amneziaproto.Credential{}).ProtoReflect().Descriptor().Fields()
	if field := peerFields.ByName("client_private_key"); field != nil {
		t.Fatal("agent change list must not carry client_private_key")
	}
}

func TestEditServerConfigRoundTripCarriesRestorableLogicalStateOnly(t *testing.T) {
	t.Parallel()

	in := &amneziaproto.ServerMessage{Type: "edit_server_config", RequestId: "req-config", EditServerConfig: &amneziaproto.EditServerConfig{NodeId: "node-1", Revision: 55, ServerConfig: &amneziaproto.ServerConfig{Jc: 1, Jmin: 2, Jmax: 3, S1: 4, S2: 5, S3: 6, S4: 7, H1: "8", H2: "9", H3: "10", H4: "11", I1: "<r 12>", I2: "<r 13>", I3: "<r 14>", I4: "<r 15>", I5: "<r 16>", ServerPrivateKey: "private", ServerIpv4: "10.8.1.1", ClientIpv4Cidr: "10.8.1.0/24", ListenPort: 51820}}}
	data, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal edit server config: %v", err)
	}
	var out amneziaproto.ServerMessage
	if err := proto.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal edit server config: %v", err)
	}
	command := out.GetEditServerConfig()
	config := command.GetServerConfig()
	if command.GetRevision() != 55 || config.GetJc() != 1 || config.GetI5() != "<r 16>" || config.GetServerPrivateKey() != "private" || config.GetServerIpv4() != "10.8.1.1" || config.GetClientIpv4Cidr() != "10.8.1.0/24" || config.GetListenPort() != 51820 {
		t.Fatalf("edit server config = %#v", command)
	}
	contentFields := (&amneziaproto.ServerConfig{}).ProtoReflect().Descriptor().Fields()
	for _, forbidden := range []protoreflect.Name{"container_runtime", "container_name", "config_path", "interface_name"} {
		if field := contentFields.ByName(forbidden); field != nil {
			t.Fatalf("edit server config content unexpectedly carries immutable field %s", forbidden)
		}
	}
}

func TestAmneziaAuthRefreshResultRoundTrip(t *testing.T) {
	t.Parallel()

	in := &commonproto.AgentMessage{
		Type:      "auth_refresh_result",
		RequestId: "req-2",
		AuthRefreshResult: &commonproto.AuthRefreshResult{
			NodeId:                   "node-1",
			Status:                   "applied",
			AppliedSnapshotVersionMs: 43,
		},
	}

	data, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("marshal apply result: %v", err)
	}

	var out commonproto.AgentMessage
	if err := proto.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal apply result: %v", err)
	}

	result := out.GetAuthRefreshResult()
	if result.GetStatus() != "applied" || result.GetAppliedSnapshotVersionMs() != 43 {
		t.Fatalf("result = %#v", result)
	}
}

func TestExistingHysteriaStreamFieldsRemainWireCompatible(t *testing.T) {
	t.Parallel()

	agentFields := (&commonproto.AgentMessage{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, agentFields, "auth_refresh_result", 8)
	assertFieldNumber(t, agentFields, "auth_manifest_request", 9)

	serverFields := (&hysteriaproto.ServerMessage{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, serverFields, "auth_refresh", 3)
	assertFieldNumber(t, serverFields, "auth_manifest_response", 6)

	refreshFields := (&commonproto.AuthRefresh{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, refreshFields, "snapshot_version_ms", 6)

	refreshResultFields := (&commonproto.AuthRefreshResult{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, refreshResultFields, "applied_snapshot_version_ms", 6)

	manifestRequestFields := (&commonproto.AuthManifestRequest{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, manifestRequestFields, "page_size", 4)
	assertFieldNumber(t, manifestRequestFields, "from_connection_id", 5)

	manifestResponseFields := (&hysteriaproto.AuthManifestResponse{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, manifestResponseFields, "changes", 5)
	assertFieldNumber(t, manifestResponseFields, "error", 6)

	heartbeatFields := (&commonproto.Heartbeat{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, heartbeatFields, "runtime_status", 4)
}

func assertFieldNumber(t *testing.T, fields protoreflect.FieldDescriptors, name protoreflect.Name, want protoreflect.FieldNumber) {
	t.Helper()

	field := fields.ByName(name)
	if field == nil {
		t.Fatalf("missing field %s", name)
	}
	if field.Number() != want {
		t.Fatalf("field %s number = %d, want %d", name, field.Number(), want)
	}
}
