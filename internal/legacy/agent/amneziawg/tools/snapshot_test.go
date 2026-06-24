package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"laz/internal/legacy/agent/amneziawg/container"
)

const nativeSeed = `[Interface]
PrivateKey = private-key
HeaderProtectionKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=
S1 = 12
S2 = 12
S3 = 12
S4 = 12

[Peer]
PublicKey = old-peer
PresharedKey = old-psk
AllowedIPs = 10.8.1.2/32

[Peer]
PublicKey = keep-peer
PresharedKey = keep-psk
AllowedIPs = 10.8.1.3/32
`

func snapshotFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "awg0.conf")
	if err := os.WriteFile(path, []byte(nativeSeed), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readSnapshot(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestDesiredSnapshotPersistsBeforeRuntimeFailureAndRestoresOffline(t *testing.T) {
	ctx := context.Background()
	path := snapshotFixture(t)
	runner := &fakeRunner{errs: []error{errors.New("runtime unavailable")}}
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(runner))))
	err := facade.SetPeer(ctx, "new-peer", PeerOptions{AllowedIPs: []string{"10.8.1.4/32"}, PresharedKey: "new-psk", PersistentKeepalive: 25})
	if err == nil {
		t.Fatal("expected runtime error")
	}
	raw := readSnapshot(t, path)
	for _, value := range []string{"new-peer", "new-psk", "keep-psk", "old-psk", "HeaderProtectionKey"} {
		if !strings.Contains(raw, value) {
			t.Fatalf("snapshot lost %q", value)
		}
	}
	if strings.Contains(raw, "SaveConfig") {
		t.Fatal("runtime takeover enabled")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("snapshot permissions: %v %v", info, err)
	}

	// A fresh agent has no DB credential material and no control-plane client.
	restored := &fakeRunner{}
	fresh := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(restored))))
	if err := fresh.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	if len(restored.argvs) != 2 || strings.Join(restored.argvs[1], " ") != "awg syncconf awg0 /dev/stdin" {
		t.Fatalf("argv=%q", restored.argvs)
	}
	stdin := string(restored.stdins[1])
	for _, value := range []string{"new-psk", "keep-psk", "old-psk"} {
		if !strings.Contains(stdin, value) {
			t.Fatalf("offline restore lost %q", value)
		}
	}
	for _, value := range []string{"PostUp", "Address", "DNS", "Table", "MTU", "SaveConfig"} {
		if strings.Contains(stdin, value) {
			t.Fatalf("syncconf received quick-only setting %q", value)
		}
	}
}

func TestSnapshotRemovalAndAllowedIPsRetainOtherPeerMaterial(t *testing.T) {
	path := snapshotFixture(t)
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(&fakeRunner{}))))
	ctx := context.Background()
	if err := facade.SetPeerAllowedIPs(ctx, "old-peer", []string{"0.0.0.0/32"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	peers, _ := cfg.SectionsByName("Peer")
	if len(peers) != 2 || peers[0].Key("PresharedKey").Value() != "old-psk" || peers[0].Key("AllowedIPs").Value() != "0.0.0.0/32" {
		t.Fatal("allowed IP update damaged peers")
	}
	if err := facade.RemovePeer(ctx, "old-peer"); err != nil {
		t.Fatal(err)
	}
	if err := facade.RemovePeer(ctx, "old-peer"); err != nil {
		t.Fatal(err)
	}
	raw := readSnapshot(t, path)
	if strings.Contains(raw, "old-peer") || strings.Contains(raw, "old-psk") || !strings.Contains(raw, "keep-psk") {
		t.Fatal("incorrect durable removal")
	}
}

func TestSnapshotFailurePreventsRuntimeMutation(t *testing.T) {
	path := snapshotFixture(t)
	if err := os.WriteFile(path, []byte("[Interface\nPrivateKey = do-not-leak"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(runner))))
	err := facade.SetPeer(context.Background(), "peer", PeerOptions{PresharedKey: "secret"})
	if err == nil || strings.Contains(err.Error(), "do-not-leak") || len(runner.argvs) != 0 {
		t.Fatalf("err=%v calls=%d", err, len(runner.argvs))
	}
	if raw := readSnapshot(t, path); raw != "[Interface\nPrivateKey = do-not-leak" {
		t.Fatal("failed write changed original")
	}
}

func TestSnapshotPrunesPendingPeersAndPersistsNativeInterface(t *testing.T) {
	path := snapshotFixture(t)
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(&fakeRunner{showconf: strings.Replace(nativeSeed, "private-key", "new-private", 1)}))))
	ctx := context.Background()
	if err := facade.PrunePeers(ctx, map[string]bool{"keep-peer": true}); err != nil {
		t.Fatal(err)
	}
	if err := facade.SetInterfaceConfig(ctx, InterfaceConfig{PrivateKey: "new-private", HeaderProtectionKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=", Address: "10.99.1.1/24", ListenPort: 51820, S1: 12, S2: 12, S3: 12, S4: 12}); err != nil {
		t.Fatal(err)
	}
	raw := readSnapshot(t, path)
	for _, value := range []string{"keep-psk", "new-private", "HeaderProtectionKey"} {
		if !strings.Contains(raw, value) {
			t.Fatalf("lost %q", value)
		}
	}
	if strings.Contains(raw, "old-peer") || strings.Contains(raw, "SaveConfig") || strings.Contains(raw, "Address") {
		t.Fatal("stale desired state retained")
	}
}

func TestRestoreFailureRedactsAllSnapshotSecrets(t *testing.T) {
	path := snapshotFixture(t)
	runner := &fakeRunner{results: []container.Result{{Stderr: "private-key old-psk keep-psk"}}, errs: []error{errors.New("restore failed")}}
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(runner))))
	err := facade.Restore(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	for _, secret := range []string{"private-key", "old-psk", "keep-psk"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("restore error leaked secret")
		}
	}
}

func TestPeerSnapshotWithoutAddressDoesNotChangeInterfaceAddress(t *testing.T) {
	path := snapshotFixture(t)
	runner := &fakeRunner{}
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(runner))))
	if err := facade.SetPeer(context.Background(), "keep-peer", PeerOptions{AllowedIPs: []string{"10.8.1.3/32"}}); err != nil {
		t.Fatal(err)
	}
	if err := facade.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readSnapshot(t, path), "Address") {
		t.Fatal("invented an interface address")
	}
	for _, argv := range runner.argvs {
		if len(argv) > 0 && argv[0] == "ip" {
			t.Fatal("restore modified host address")
		}
	}
}

func TestFailedRemovalIsDurableAndOfflineRestoreDoesNotResurrectPeer(t *testing.T) {
	path := snapshotFixture(t)
	runner := &fakeRunner{errs: []error{errors.New("interface down")}}
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(runner))))
	if err := facade.RemovePeer(context.Background(), "old-peer"); err == nil {
		t.Fatal("expected runtime failure")
	}
	freshRunner := &fakeRunner{}
	fresh := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(freshRunner))))
	if err := fresh.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	stdin := string(freshRunner.stdins[0])
	if strings.Contains(stdin, "old-peer") || !strings.Contains(stdin, "keep-psk") {
		t.Fatalf("incorrect recovery: %q", stdin)
	}
}

func TestSnapshotsRejectQuickSettingsBeforePrivilegedCommands(t *testing.T) {
	for _, setting := range []string{"PostUp = touch /root/injected", "PostDown = reboot", "PreUp = id", "PreDown = id", "SaveConfig = true", "Address = 10.8.1.1/24", "DNS = 1.1.1.1", "MTU = 1280", "Table = off"} {
		t.Run(strings.Fields(setting)[0], func(t *testing.T) {
			path := snapshotFixture(t)
			raw := strings.Replace(nativeSeed, "[Interface]\n", "[Interface]\n"+setting+"\n", 1)
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			runner := &fakeRunner{}
			facade := New(WithConfigPath(path), WithSudo(true), WithRuntime(container.New(container.WithRunner(runner))))
			if err := facade.Restart(context.Background()); err == nil {
				t.Fatal("accepted shell-capable snapshot")
			}
			if err := facade.Restore(context.Background()); err == nil {
				t.Fatal("accepted non-native snapshot")
			}
			if len(runner.argvs) != 0 {
				t.Fatal("issued privileged command before rejecting snapshot")
			}
			if readSnapshot(t, path) != raw {
				t.Fatal("rewrote rejected snapshot")
			}
		})
	}
}

func TestMissingSnapshotSeedsFromLocalShowconfOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "awg0.conf")
	runner := &fakeRunner{results: []container.Result{{Stdout: nativeSeed}}}
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(runner))))
	if err := facade.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.argvs) != 2 || strings.Join(runner.argvs[0], " ") != "awg showconf awg0" {
		t.Fatalf("argv=%q", runner.argvs)
	}
	if !strings.Contains(readSnapshot(t, path), "private-key") {
		t.Fatal("seed lost secret key")
	}
	if err := facade.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.argvs) != 3 || strings.Contains(strings.Join(runner.argvs[2], " "), "showconf") {
		t.Fatal("reseeded existing desired state from runtime")
	}
}

func TestNativeSnapshotContentPadding(t *testing.T) {
	path := snapshotFixture(t)
	runner := &fakeRunner{showconf: strings.Replace(nativeSeed, "[Interface]", "[Interface]\nContentPaddingAddition = 32-128", 1)}
	facade := New(WithConfigPath(path), WithRuntime(container.New(container.WithRunner(runner))))
	config := InterfaceConfig{PrivateKey: "private", HeaderProtectionKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=", Address: "10.8.1.1/24", ListenPort: 51820, S1: 12, S2: 12, S3: 12, S4: 12, ContentPaddingAddition: "32-128"}
	if err := facade.SetInterfaceConfig(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readSnapshot(t, path), "ContentPaddingAddition = 32-128") {
		t.Fatal("missing padding in snapshot")
	}
	if !strings.Contains(strings.Join(runner.argvs[0], " "), "content-padding-addition 32-128") {
		t.Fatal("missing runtime padding")
	}
	for _, value := range []string{"128-32", "65536", "1-2-3", "bad"} {
		config.ContentPaddingAddition = value
		if err := facade.SetInterfaceConfig(context.Background(), config); err == nil {
			t.Fatalf("accepted padding %q", value)
		}
	}
}
