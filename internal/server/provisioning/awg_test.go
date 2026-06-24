package provisioning

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"laz/internal/server/integrations/nativeawg"
)

func TestAWGPollRebootResume(t *testing.T) {
	for _, tc := range []struct {
		name, out, next string
		failure         bool
	}{
		{"still running", "boot-a\n2\nrunning\n", "awg.install.poll", false},
		{"SSH not reboot proof", "boot-a\n2\n1\n", "awg.install.poll", false},
		{"reboot one", "boot-b\n2\n1\n", "awg.install.launch", false},
		{"reboot two", "boot-b\n3\n1\n", "awg.install.launch", false},
		{"complete", "boot-a\n99\n0\n", "awg.install.handoff", false},
		{"complete with removed state", "boot-a\nabsent\n0\n", "awg.install.handoff", false},
		{"missing state while running", "boot-a\nabsent\nrunning\n", "awg.install.poll", false},
		{"missing state with failure", "boot-a\nabsent\n42\n", "", true},
		{"unfinished state with zero exit", "boot-a\n3\n0\n", "", true},
		{"DKMS failure", "boot-a\n2\n42\n", "", true},
		{"corrupt state", "oops", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := &Installer{awgExecute: func(context.Context, AWGState, string) (string, error) { return tc.out, nil }}
			s := AWGState{BootID: "boot-a", Deadline: time.Now().Add(time.Hour)}
			r, err := i.ExecuteAWGStep(context.Background(), "awg.install.poll", s)
			if (err != nil) != tc.failure || r.Next != tc.next {
				t.Fatalf("next=%q err=%v", r.Next, err)
			}
			if tc.next == "awg.install.launch" && r.State.BootID != "boot-b" {
				t.Fatal("new boot ID not persisted")
			}
		})
	}
}

func TestAWGDisconnectAndLimits(t *testing.T) {
	i := &Installer{awgExecute: func(context.Context, AWGState, string) (string, error) {
		return "", &awgConnectionError{errors.New("EOF")}
	}}
	s := AWGState{Deadline: time.Now().Add(time.Hour)}
	r, err := i.ExecuteAWGStep(context.Background(), "awg.install.poll", s)
	if err != nil || r.Next != "awg.install.poll" || r.Wait == 0 {
		t.Fatalf("disconnect must poll: %+v %v", r, err)
	}
	s.Runs = 4
	if _, err = i.ExecuteAWGStep(context.Background(), "awg.install.launch", s); err == nil {
		t.Fatal("unbounded resume")
	}
	s.Deadline = time.Now().Add(-time.Minute)
	if _, err = i.ExecuteAWGStep(context.Background(), "awg.install.poll", s); err == nil || !strings.Contains(err.Error(), "REBOOT_TIMEOUT") {
		t.Fatal(err)
	}
}

func TestAWGImportRetainsServerIdentity(t *testing.T) {
	private, public, err := nativeawg.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	hpk, err := nativeawg.GeneratePresharedKey()
	if err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf("[Interface]\nPrivateKey = %s\nHeaderProtectionKey = %s\nContentPaddingAddition = 32-128\nAddress = 10.8.1.1/24\nListenPort = 51820\nMTU = 1280\nJc = 6\nJmin = 40\nJmax = 70\nS1 = 15\nS2 = 16\nS3 = 17\nS4 = 18\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n[Peer]\nPrivateKey = WRONG\n", private, hpk)
	in := AWGInstallInput{Subnet: "10.8.1.0/24", EndpointHost: "vpn.example", ListenPort: 51820}
	c, err := importAWGConfig(raw, in)
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerPrivateKey != private || c.ServerPublicKey != public || c.HeaderProtectionKey != hpk || c.MTU != 1280 || c.ContentPaddingAddition != "32-128" {
		t.Fatal("identity not retained")
	}
	for _, tc := range []struct{ subnet, address, first, last string }{
		{"10.8.0.0/16", "10.8.0.1/16", "10.8.0.2", "10.8.255.254"},
		{"10.8.1.128/25", "10.8.1.129/25", "10.8.1.130", "10.8.1.254"},
		{"10.8.1.252/30", "10.8.1.253/30", "10.8.1.254", "10.8.1.254"},
	} {
		input := in
		input.Subnet = tc.subnet
		config, err := importAWGConfig(strings.Replace(raw, "10.8.1.1/24", tc.address, 1), input)
		if err != nil {
			t.Fatalf("%s: %v", tc.subnet, err)
		}
		if config.FirstClientIPv4 != tc.first || config.LastClientIPv4 != tc.last {
			t.Fatalf("wrong imported pool for %s", tc.subnet)
		}
	}
	for _, bad := range []string{strings.Replace(raw, "S1 = 15", "S1 = 2", 1), strings.Replace(raw, "Address = 10.8.1.1/24", "Address = 10.9.0.1/24", 1), strings.Replace(raw, "[Peer]", "I1 = 1\n[Peer]", 1)} {
		if _, err := importAWGConfig(bad, in); err == nil {
			t.Fatal("accepted incompatible config")
		}
	}
}

func TestAWGSubnetOverlap(t *testing.T) {
	subnet := netip.MustParsePrefix("10.8.1.0/24")
	for _, tc := range []struct {
		raw string
		bad bool
	}{{`[{"dst":"default"},{"dst":"192.168.1.0/24"}]`, false}, {`[{"dst":"10.0.0.0/8"}]`, true}, {`[{"dst":"10.8.1.128/25"}]`, true}, {`bad`, true}} {
		if err := checkAWGRoutes(tc.raw, subnet); (err != nil) != tc.bad {
			t.Fatal(tc.raw, err)
		}
	}
}

func TestAWGProductionScripts(t *testing.T) {
	s := AWGState{Input: AWGInstallInput{NodeName: "server ' quoted", Subnet: "10.8.1.0/24", ListenPort: 51820}}
	scripts := []string{awgPreflight(s.Input), awgPrepareScript(s), awgLaunchScript(s), awgHandoffScript(s), awgVerifyScript(s)}
	for n, script := range scripts {
		cmd := exec.Command("sh", "-n")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("script %d syntax: %s %v", n, out, err)
		}
	}
	launch := awgLaunchScript(s)
	for _, flag := range []string{"--protocol=3.1", "--no-tweaks", "--allow-ipv6", "--no-cps"} {
		if !strings.Contains(launch, flag) {
			t.Fatal(flag)
		}
	}
	if strings.Contains(launch, "AWG_BRANCH=") || strings.Contains(launch, "--force") {
		t.Fatal("unsafe installer override")
	}
	unit := awgAgentUnit()
	if strings.Contains(unit, "ReadWritePaths=/etc") || strings.Contains(unit, "/etc/amnezia/amneziawg /dev/shm") {
		t.Fatal("agent can edit root hooks")
	}
	if !strings.Contains(awgHandoffScript(s), "ExecStartPost=/usr/bin/awg syncconf") {
		t.Fatal("offline runtime restart loses peers")
	}
}

func TestAWGAgentInstallerUsesStructuredConfiguration(t *testing.T) {
	i := &Installer{}
	s := AWGState{ServerURL: "https://panel.example", AgentTarget: "panel.example:9443", DownloadBase: "https://downloads.example/v1"}
	s.Identity.NodeID = "node-id"
	s.Identity.InstallationID = "installation-id"
	script, err := i.awgAgentScript(s)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("agent script: %s %v", out, err)
	}
	if strings.Contains(script, "hysteria2:") || strings.Contains(script, h2AgentBinaryName) || strings.Contains(script, h2AgentUser) {
		t.Fatal("AWG installer depends on Hysteria string substitutions")
	}
	_, config, ok := strings.Cut(script, "<<'EOF_AGENT_CONFIG'\n")
	if !ok {
		t.Fatal("missing agent config")
	}
	config, _, _ = strings.Cut(config, "\nEOF_AGENT_CONFIG")
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(config), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["node_id"] != "node-id" || parsed["agent_type"] != "amneziawg" || parsed["amneziawg"] == nil || parsed["hysteria2"] != nil {
		t.Fatal("invalid AWG agent config")
	}
}
