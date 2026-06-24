package tools

import (
	"context"
	"errors"
	"laz/internal/legacy/agent/amneziawg/container"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type normalizedFirewallRunner struct{ argvs [][]string }

func (r *normalizedFirewallRunner) Run(_ context.Context, argv []string, _ []byte, _ int) (container.Result, error) {
	r.argvs = append(r.argvs, append([]string(nil), argv...))
	if slices.Contains(argv, "-S") {
		return container.Result{Stdout: `-A INPUT -p udp -m udp --dport 51900 -m comment --comment "lazgate-awg" -j ACCEPT
-A INPUT -p udp -m udp --dport 51820 -m comment --comment "lazgate-awg" -j ACCEPT
-A FORWARD -s 10.8.0.0/23 -i awg0 -m comment --comment "lazgate-awg" -j ACCEPT
-A INPUT -p tcp --dport 22 -j ACCEPT
`}, nil
	}
	return container.Result{}, nil
}

func TestFirewallCleanupUnderstandsNormalizedRules(t *testing.T) {
	host := filepath.Join(t.TempDir(), "awg0.conf")
	if err := os.WriteFile(host, []byte("[Interface]\nAddress = 10.8.1.1/23\nListenPort = 51820\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &normalizedFirewallRunner{}
	f := New(WithHostConfigPath(host), WithRuntime(container.New(container.WithRunner(runner))))
	if err := f.applyHostNetwork(context.Background(), hostNetworkConfig{Address: "10.8.1.1/23", ListenPort: 51900}); err != nil {
		t.Fatal(err)
	}
	removedOld := false
	for _, args := range runner.argvs {
		if !slices.Contains(args, "-D") {
			continue
		}
		line := strings.Join(args, " ")
		if strings.Contains(line, "51900") || strings.Contains(line, "-D FORWARD") || strings.Contains(line, "--dport 22") {
			t.Fatalf("removed current or unrelated rule: %s", line)
		}
		removedOld = removedOld || strings.Contains(line, "51820")
	}
	if !removedOld {
		t.Fatal("old owned port rule retained")
	}
}

func TestSettingsPersistNativeAndBootConfiguration(t *testing.T) {
	path := snapshotFixture(t)
	host := filepath.Join(t.TempDir(), "awg0.conf")
	if err := os.WriteFile(host, []byte("[Interface]\nPrivateKey = old\nAddress = 10.8.1.1/24\nListenPort = 51820\n[Peer]\nPublicKey = keep-peer\nAllowedIPs = 10.8.1.2/32\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{showconf: "[Interface]\nPrivateKey = tool-private\nHeaderProtectionKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=\nListenPort = 51900\nH1 = 100-200\nI1 = <r 2><b 0x1234>\nRekeyTimeout = 2-5\n"}
	f := New(WithConfigPath(path), WithHostConfigPath(host), WithRuntime(container.New(container.WithRunner(runner))))
	c := InterfaceConfig{PrivateKey: "new-private", HeaderProtectionKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=", Address: "10.8.1.1/23", ListenPort: 51900, S1: 12, S2: 12, S3: 12, S4: 12, H1: "100-200", H2: "2", H3: "3", H4: "4", I1: "<r 2><b 0x1234>", Timings: map[string]string{"RekeyTimeout": "2-5"}, RandomTrailers: true}
	if err := f.SetInterfaceConfig(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"tool-private", "100-200", "<r 2><b 0x1234>", "2-5", "51900"} {
			if !strings.Contains(string(data), expected) {
				t.Fatalf("%s missing %s", p, expected)
			}
		}
		stat, _ := os.Stat(p)
		if stat.Mode().Perm() != 0600 {
			t.Fatal("insecure mode")
		}
	}
	boot, _ := os.ReadFile(host)
	if strings.Contains(string(boot), "PostUp = `") || strings.Contains(string(boot), "PostDown = `") {
		t.Fatal("deployment hooks must remain literal shell commands")
	}
	if !strings.Contains(string(boot), "PrivateKey") || strings.Contains(string(boot), "new-private") || strings.Contains(string(boot), "100-200") {
		t.Fatal("deployment must not write protocol fields")
	}
	if !slices.Equal(runner.argvs[len(runner.argvs)-1], []string{"awg-quick", "save", host}) {
		t.Fatal("boot protocol config must be saved by awg-quick")
	}
	if !strings.Contains(string(boot), "10.8.1.1/23") || !strings.Contains(string(boot), "lazgate-awg") || !strings.Contains(string(boot), "keep-peer") {
		t.Fatal("boot config lost network or peers")
	}
}

func TestFailedInterfaceApplyKeepsPreviousDurableSettings(t *testing.T) {
	path := snapshotFixture(t)
	before := readSnapshot(t, path)
	f := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(&fakeRunner{errs: []error{errors.New("runtime failed")}}))))
	err := f.SetInterfaceConfig(context.Background(), InterfaceConfig{PrivateKey: "new-private", HeaderProtectionKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=", Address: "10.8.1.1/24", ListenPort: 51820, S1: 12, S2: 12, S3: 12, S4: 12})
	if err == nil || readSnapshot(t, path) != before {
		t.Fatal("failed apply modified durable settings")
	}
}

func TestPeerCommandsSaveThroughQuickTool(t *testing.T) {
	for _, operation := range []struct {
		name string
		run  func(*Facade) error
	}{
		{"create", func(f *Facade) error {
			return f.SetPeer(context.Background(), "peer", PeerOptions{AllowedIPs: []string{"10.8.1.2/32"}})
		}},
		{"block", func(f *Facade) error {
			return f.SetPeerAllowedIPs(context.Background(), "peer", []string{"0.0.0.0/32"})
		}},
		{"delete", func(f *Facade) error { return f.RemovePeer(context.Background(), "peer") }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			runner := &fakeRunner{}
			f := New(WithHostConfigPath("/etc/amnezia/amneziawg/awg0.conf"), WithRuntime(container.New(container.WithRunner(runner))), WithAWGBinaryPath("/usr/local/bin/awg"), WithSudo(true))
			if err := operation.run(f); err != nil {
				t.Fatal(err)
			}
			want := []string{"sudo", "-n", "/usr/local/bin/awg-quick", "save", "/etc/amnezia/amneziawg/awg0.conf"}
			if !slices.Equal(runner.argvs[len(runner.argvs)-1], want) {
				t.Fatalf("missing native save: %q", runner.argvs)
			}
			runner.saveError = errors.New("save failed")
			if err := operation.run(f); err == nil {
				t.Fatal("save failure ignored")
			}
		})
	}
}

func TestShowconfFailurePreservesDesiredSnapshot(t *testing.T) {
	path := snapshotFixture(t)
	before := readSnapshot(t, path)
	runner := &fakeRunner{showconf: "invalid native output"}
	f := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(runner))))
	if err := f.snapshotInterface(context.Background()); err == nil {
		t.Fatal("invalid showconf accepted")
	}
	if readSnapshot(t, path) != before {
		t.Fatal("showconf failure changed desired snapshot")
	}
}
