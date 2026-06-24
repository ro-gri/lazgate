package nativeawg

import (
	"strings"
	"testing"

	"laz/internal/server/model"
)

func TestRenderClientConfContainsCompleteAmneziaWGParameters(t *testing.T) {
	config := completeNodeConfig()
	conf, err := RenderClientConf(config, model.AmneziaWGConnectionMaterial{
		ClientPrivateKey: "client-private",
		PresharedKey:     "preshared",
		AssignedIPv4:     "10.8.1.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Jc = 4", "Jmin = 40", "S4 = 22", "H4 = 400", "I5 = <r 55>", "RandomTrailers = off", "DisableCookies = off"} {
		if !strings.Contains(conf, expected) {
			t.Fatalf("client config does not contain %q:\n%s", expected, conf)
		}
	}
}

func TestEditServerConfigContainsCompleteAmneziaWGParameters(t *testing.T) {
	config := completeNodeConfig()
	config.NodeID = "node-1"
	config.ConfigVersionMS = 123
	command, err := EditServerConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := command.GetServerConfig()
	if command.GetNodeId() != "node-1" || command.GetRevision() != 123 || serverConfig.GetJc() != 4 || serverConfig.GetI5() != "<r 55>" || serverConfig.GetServerPrivateKey() != "server-private" || serverConfig.GetServerIpv4() != "10.8.1.1" || serverConfig.GetClientIpv4Cidr() != "10.8.1.0/24" || serverConfig.GetListenPort() != 51820 {
		t.Fatalf("unexpected edit command: %+v", command)
	}
}

func TestValidateNodeConfigRejectsMissingAmneziaWGParameter(t *testing.T) {
	config := completeNodeConfig()
	config.I5 = "<r 0>"
	if err := ValidateNodeConfig(config); err == nil || !strings.Contains(err.Error(), "I5") {
		t.Fatalf("expected i5 validation error, got %v", err)
	}
}

func completeNodeConfig() model.AmneziaWGNodeConfig {
	return model.AmneziaWGNodeConfig{
		EndpointHost: "awg.example", EndpointPort: 51820, ListenPort: 51820,
		ServerIPv4: "10.8.1.1", ClientIPv4CIDR: "10.8.1.0/24", FirstClientIPv4: "10.8.1.2", LastClientIPv4: "10.8.1.254",
		DNS: "1.1.1.1", MTU: 1376, PersistentKeepalive: 25, ClientRouteAllowedIPs: "0.0.0.0/0",
		ServerPrivateKey: "server-private", ServerPublicKey: "server-public",
		JC: 4, JMin: 40, JMax: 70, S1: 16, S2: 97, S3: 33, S4: 22,
		H1: "100", H2: "200", H3: "300", H4: "400", I1: "<r 11>", I2: "<r 22>", I3: "<r 33>", I4: "<r 44>", I5: "<r 55>",
	}
}
