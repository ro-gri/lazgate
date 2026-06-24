package provisioning

import (
	"context"
	"fmt"
	"testing"
	"time"

	"laz/internal/server/amneziasettings"
	"laz/internal/server/integrations/nativeawg"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func TestAWGHandoffAppliesInstallationSettingsWithoutReplacingServerIdentity(t *testing.T) {
	private, public, err := nativeawg.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := nativeawg.GeneratePresharedKey()
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := nativeawg.GeneratePresharedKey()
	if err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf("[Interface]\nPrivateKey = %s\nHeaderProtectionKey = %s\nAddress = 10.8.1.1/24\nListenPort = 51820\nMTU = 1280\nJc = 4\nJmin = 40\nJmax = 70\nS1 = 12\nS2 = 12\nS3 = 12\nS4 = 12\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n", private, oldKey)
	settings := &amneziasettings.Input{EndpointHost: "vpn.example.com", EndpointPort: 51820, DNS: "9.9.9.9", ServerIPv4: "10.8.1.254", Prefix: 24, MTU: 1280, Keepalive: 30, JC: 5, JMin: 40, JMax: 70, S1: 32, S2: 48, S3: 16, S4: 16, H1: "100-200", H2: "2", H3: "3", H4: "4", I1: "<r 8>", HeaderKey: &newKey, RouteMode: "all", IPv6Mode: "off", Timings: map[string]string{"RekeyTimeout": "2-5"}}
	s := AWGState{Input: AWGInstallInput{Settings: settings, EndpointHost: "vpn.example.com", ListenPort: 51820, Subnet: "10.8.1.0/24"}, Identity: InstallState{NodeID: "node"}, Deadline: time.Now().Add(time.Hour)}
	i := &Installer{awgExecute: func(context.Context, AWGState, string) (string, error) { return raw, nil }}
	result, err := i.ExecuteAWGStep(context.Background(), "awg.install.handoff", s)
	if err != nil {
		t.Fatal(err)
	}
	c := result.State.Config
	if c.NodeID != "node" || c.ServerPrivateKey != private || c.ServerPublicKey != public || c.HeaderProtectionKey != newKey || c.I1 != "<r 8>" || c.H1 != "100-200" || c.ServerIPv4 != "10.8.1.254" || c.DNS != "9.9.9.9" {
		t.Fatal("settings or server identity lost")
	}
	settings.HeaderKey = nil
	result, err = i.ExecuteAWGStep(context.Background(), "awg.install.handoff", s)
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Config.HeaderProtectionKey != oldKey {
		t.Fatal("installer key not preserved")
	}
}

type waitSettingsStore struct {
	store.Store
	config model.AmneziaWGNodeConfig
}

func (s waitSettingsStore) GetNodeRuntime(string) (model.NodeRuntime, error) {
	return model.NodeRuntime{AgentStatus: "online", ServiceStatus: "active", LastHeartbeatAt: time.Now()}, nil
}
func (s waitSettingsStore) GetAmneziaWGNodeConfig(string) (model.AmneziaWGNodeConfig, error) {
	return s.config, nil
}

func TestAWGInstallationWaitsForAppliedSettings(t *testing.T) {
	s := AWGState{Input: AWGInstallInput{Settings: &amneziasettings.Input{}}, Identity: InstallState{NodeID: "node"}, Deadline: time.Now().Add(time.Hour)}
	for _, tc := range []struct {
		name    string
		applied int64
		errorID string
		next    string
		fail    bool
	}{
		{"pending", 0, "", "awg.install.wait_agent", false},
		{"applied", 10, "", "awg.install.done", false},
		{"failed", 0, "error-id", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := New(waitSettingsStore{config: model.AmneziaWGNodeConfig{ConfigVersionMS: 10, AppliedConfigVersionMS: tc.applied, ConfigErrorID: tc.errorID}})
			result, err := i.ExecuteAWGStep(context.Background(), "awg.install.wait_agent", s)
			if (err != nil) != tc.fail || !tc.fail && result.Next != tc.next {
				t.Fatalf("next=%s err=%v", result.Next, err)
			}
		})
	}
}
