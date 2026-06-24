package protocol_test

import (
	"os"
	"strings"
	"testing"

	commonproto "laz/generated/nodeproto/agentcommon"
	amneziaproto "laz/generated/nodeproto/amnezia"
	hysteriaproto "laz/generated/nodeproto/hysteria"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestHysteriaProtoHasNoAmneziaDependency(t *testing.T) {
	raw, err := os.ReadFile("../../generated/proto/hysteria/agent.proto")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"amnezia.proto", "AmneziaWG", "amneziawg", "AmneziaWGServerConfigContent"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("hysteria agent proto still contains %q", forbidden)
		}
	}

	agentFields := (&commonproto.AgentMessage{}).ProtoReflect().Descriptor().Fields()
	assertNoField(t, agentFields, "amneziawg_apply_result")
	serverFields := (&hysteriaproto.ServerMessage{}).ProtoReflect().Descriptor().Fields()
	assertNoField(t, serverFields, "amneziawg_changes")
}

func TestAmneziaAgentStreamSurface(t *testing.T) {
	if got := amneziaproto.Agent_ServiceDesc.ServiceName; got != "laz.nodeproto.amnezia.Agent" {
		t.Fatalf("Amnezia service name = %q", got)
	}
	if len(amneziaproto.Agent_ServiceDesc.Streams) != 1 || amneziaproto.Agent_ServiceDesc.Streams[0].StreamName != "Connect" {
		t.Fatalf("Amnezia streams = %#v", amneziaproto.Agent_ServiceDesc.Streams)
	}
	serverFields := (&amneziaproto.ServerMessage{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, serverFields, "auth_refresh", 3)
	assertFieldNumber(t, serverFields, "runtime_command", 4)
	assertFieldNumber(t, serverFields, "usage_ack", 5)
	assertFieldNumber(t, serverFields, "auth_manifest_response", 6)
	assertFieldNumber(t, serverFields, "edit_server_config", 7)
	agentFields := (&commonproto.AgentMessage{}).ProtoReflect().Descriptor().Fields()
	assertFieldNumber(t, agentFields, "heartbeat", 4)
	assertFieldNumber(t, agentFields, "auth_refresh_result", 8)
	assertFieldNumber(t, agentFields, "auth_manifest_request", 9)
	assertFieldNumber(t, agentFields, "server_config_apply_result", 10)
}

func assertNoField(t *testing.T, fields protoreflect.FieldDescriptors, name protoreflect.Name) {
	t.Helper()
	if field := fields.ByName(name); field != nil {
		t.Fatalf("unexpected field %s at number %d", name, field.Number())
	}
}
