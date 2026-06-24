package tools

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"laz/internal/legacy/agent/amneziawg/container"
)

type fakeRunner struct {
	showconf  string
	saveError error
	results   []container.Result
	errs      []error
	argvs     [][]string
	stdins    [][]byte
}

func (f *fakeRunner) Run(_ context.Context, argv []string, stdin []byte, _ int) (container.Result, error) {
	f.argvs = append(f.argvs, append([]string(nil), argv...))
	f.stdins = append(f.stdins, append([]byte(nil), stdin...))
	idx := len(f.argvs) - 1
	var res container.Result
	if idx < len(f.results) {
		res = f.results[idx]
	}
	if idx < len(f.errs) {
		return res, f.errs[idx]
	}
	for _, arg := range argv {
		if arg == "showconf" && f.showconf != "" {
			res.Stdout = f.showconf
		}
	}
	if len(argv) >= 3 && strings.HasSuffix(argv[len(argv)-3], "awg-quick") && argv[len(argv)-2] == "save" && f.saveError != nil {
		return res, f.saveError
	}
	return res, nil
}

func TestFacadeCommandArgvConstruction(t *testing.T) {
	runner := &fakeRunner{results: []container.Result{{Stdout: goldenDump}, {Stdout: "running\n"}, {Stdout: "logs\n"}}}
	facade := New(WithRuntime(container.New(container.WithRunner(runner))))
	if _, err := facade.ShowDump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := facade.SetPeerAllowedIPs(context.Background(), "peer", []string{"10.0.0.2/32"}); err != nil {
		t.Fatal(err)
	}
	if err := facade.RemovePeer(context.Background(), "peer"); err != nil {
		t.Fatal(err)
	}
	if err := facade.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := facade.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := facade.Logs(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"awg", "show", "awg0", "dump"},
		{"awg", "set", "awg0", "peer", "peer", "allowed-ips", "10.0.0.2/32"},
		{"awg", "set", "awg0", "peer", "peer", "remove"},
		{"systemctl", "restart", "awg-quick@awg0.service"},
		{"systemctl", "show", "--property=ActiveState", "--value", "awg-quick@awg0.service"},
		{"journalctl", "--unit", "awg-quick@awg0.service", "--lines", "80", "--no-pager"},
	}
	if !reflect.DeepEqual(runner.argvs, want) {
		t.Fatalf("argvs = %#v, want %#v", runner.argvs, want)
	}
}

func TestFacadeUsesConfiguredBinaryAndInterface(t *testing.T) {
	runner := &fakeRunner{results: []container.Result{{Stdout: goldenDump}}}
	facade := New(
		WithRuntime(container.New(container.WithRunner(runner))),
		WithAWGBinaryPath("/usr/local/bin/awg"),
		WithSudo(true),
		WithInterfaceName("awg7"),
	)
	if _, err := facade.ShowDump(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"sudo", "-n", "/usr/local/bin/awg", "show", "awg7", "dump"}}
	if !reflect.DeepEqual(runner.argvs, want) {
		t.Fatalf("argvs = %#v, want %#v", runner.argvs, want)
	}
}

func TestSetPeerStreamsPSKToHostTmpfs(t *testing.T) {
	const psk = "super-secret-psk"
	runner := &fakeRunner{}
	facade := New(WithRuntime(container.New(container.WithRunner(runner))))
	if err := facade.SetPeer(context.Background(), "peer", PeerOptions{AllowedIPs: []string{"10.0.0.2/32"}, PresharedKey: psk}); err != nil {
		t.Fatal(err)
	}
	if len(runner.argvs) != 1 {
		t.Fatalf("command count = %d, want only PSK shell command", len(runner.argvs))
	}
	argv := runner.argvs[0]
	if !reflect.DeepEqual(argv[:2], []string{"sh", "-ceu"}) {
		t.Fatalf("host shell argv = %#v", argv[:2])
	}
	if strings.Join(argv, " ") == "" || strings.Contains(strings.Join(argv, " "), "docker cp") || strings.Contains(strings.Join(argv, " "), "/tmp/") {
		t.Fatalf("unexpected command shape: %q", argv)
	}
	if strings.Contains(strings.Join(argv, " "), psk) {
		t.Fatalf("argv leaked PSK: %q", argv)
	}
	if got := string(runner.stdins[0]); got != psk+"\n" {
		t.Fatalf("stdin = %q, want PSK with newline", got)
	}
	script := argv[2]
	for _, required := range []string{
		"/dev/shm",
		"awk '$5 == \"/\" || $5 == \"/dev\" || $5 == \"/dev/shm\" { split($0, fields, \" - \"); split(fields[2], fs); visible_fstype = fs[1] } END { exit visible_fstype != \"tmpfs\" }' /proc/self/mountinfo",
		"PSK tmpfs unavailable: /dev/shm visible mount is not tmpfs; check /proc/self/mountinfo",
		"dir=$3",
		"/dev/shm/lazgate-awg-psk-[0-9a-f]*",
		"mkdir -m 700 \"$dir\"",
		"cleanup() {",
		"trap cleanup EXIT",
		"trap on_signal HUP INT TERM",
		"cat > \"$psk_path\"",
		"awg set \"$iface\" peer \"$public_key\" \"$@\" preshared-key \"$psk_path\"",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("script missing %q:\n%s", required, script)
		}
	}
	if strings.Contains(script, psk) {
		t.Fatalf("script leaked PSK: %s", script)
	}

}

func TestPSKTmpfsMountCheckUsesLastVisibleCoveringMount(t *testing.T) {
	const awkProgram = `$5 == "/" || $5 == "/dev" || $5 == "/dev/shm" { split($0, fields, " - "); split(fields[2], fs); visible_fstype = fs[1] } END { exit visible_fstype != "tmpfs" }`
	cases := []struct {
		name      string
		mountinfo string
		wantPass  bool
	}{
		{"only tmpfs", "35 24 0:29 / /dev/shm rw,nosuid,nodev - tmpfs shm rw\n", true},
		{"only persistent filesystem", "35 24 8:1 /var/shm /dev/shm rw - ext4 /dev/sda1 rw\n", false},
		{"tmpfs shadowed by persistent overmount", "35 24 0:29 / /dev/shm rw,nosuid,nodev - tmpfs shm rw\n36 24 8:1 /var/shm /dev/shm rw - ext4 /dev/sda1 rw\n", false},
		{"persistent filesystem shadowed by tmpfs exact overmount", "35 24 8:1 /var/shm /dev/shm rw - ext4 /dev/sda1 rw\n36 24 0:29 / /dev/shm rw,nosuid,nodev - tmpfs shm rw\n", true},
		{"tmpfs exact mount shadowed by persistent dev overmount", "35 24 0:29 / /dev/shm rw - tmpfs shm rw\n36 24 8:1 / /dev rw - ext4 /dev/sda1 rw\n", false},
		{"persistent dev overmount shadowed by tmpfs exact mount", "35 24 8:1 / /dev rw - ext4 /dev/sda1 rw\n36 24 0:29 / /dev/shm rw - tmpfs shm rw\n", true},
		{"persistent root mount", "35 24 8:1 / / rw - ext4 /dev/sda1 rw\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("awk", awkProgram)
			cmd.Stdin = strings.NewReader(tc.mountinfo)
			err := cmd.Run()
			if gotPass := err == nil; gotPass != tc.wantPass {
				t.Fatalf("pass = %t, want %t; err = %v", gotPass, tc.wantPass, err)
			}
		})
	}
}

func TestPSKStdinScriptCleansTmpfsDirectoryOnTERM(t *testing.T) {
	if err := exec.Command("sh", "-c", "[ -d /dev/shm ] && [ -w /dev/shm ]").Run(); err != nil {
		t.Skipf("requires writable Linux /dev/shm: %v", err)
	}
	binDir := t.TempDir()
	pskDir, err := newPSKTmpfsDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(pskDir) })
	readyPath := filepath.Join(t.TempDir(), "awg-ready")
	for name, body := range map[string]string{
		"awk": "#!/bin/sh\nexit 0\n",

		"awg": "#!/bin/sh\n: > \"$TEST_AWG_READY\"\nwhile :; do sleep 1; done\n",
	} {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sh", "-ceu", pskStdinScript, "sh", "awg", "false", "awg0", "peer", pskDir)
	cmd.Stdin = strings.NewReader("shell-test-psk\n")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "TEST_PSK_DIR="+pskDir, "TEST_AWG_READY="+readyPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(pskDir, "psk")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("PSK file was not created; output: %s", output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected non-zero exit after TERM")
	}
	if _, err := os.Stat(pskDir); !os.IsNotExist(err) {
		t.Fatalf("PSK directory remains after TERM: %v", err)
	}
}

func TestSetPeerDoesNotStartHostCleanupAfterPrimaryError(t *testing.T) {
	const psk = "super-secret-psk"
	runner := &fakeRunner{
		results: []container.Result{{Stderr: "set failed " + psk}},
		errs:    []error{errors.New("set failed " + psk)},
	}
	facade := New(WithRuntime(container.New(container.WithRunner(runner))))
	err := facade.SetPeer(context.Background(), "peer", PeerOptions{PresharedKey: psk})
	if err == nil || !strings.Contains(err.Error(), "set failed") || strings.Contains(err.Error(), psk) {
		t.Fatalf("primary error was not preserved/redacted: %v", err)
	}
	if len(runner.argvs) != 1 {
		t.Fatalf("command count = %d, want no competing host cleanup", len(runner.argvs))
	}
}

func TestSetPeerPropagatesTmpfsAndAWGErrorsWithoutPSKLeak(t *testing.T) {
	const psk = "super-secret-psk"
	for _, name := range []string{"tmpfs unavailable", "awg failed"} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{results: []container.Result{{Stderr: name + " " + psk}}, errs: []error{errors.New(name + " " + psk)}}
			facade := New(WithRuntime(container.New(container.WithRunner(runner))))
			err := facade.SetPeer(context.Background(), "peer", PeerOptions{PresharedKey: psk})
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), psk) {
				t.Fatalf("error leaked PSK: %s", err)
			}
		})
	}
}

func TestSetPeerAllowedIPsAndRemovePeerValidation(t *testing.T) {
	facade := New(WithRuntime(container.New(container.WithRunner(&fakeRunner{}))))
	if err := facade.SetPeerAllowedIPs(context.Background(), " ", []string{"10.0.0.2/32"}); err == nil {
		t.Fatal("expected empty public key error")
	}
	if err := facade.RemovePeer(context.Background(), "\t"); err == nil {
		t.Fatal("expected empty public key error")
	}
}

func TestSetPeerRejectsInvalidAllowedIPsBeforeRuntime(t *testing.T) {
	runner := &fakeRunner{}
	facade := New(WithRuntime(container.New(container.WithRunner(runner))))
	err := facade.SetPeer(context.Background(), "peer", PeerOptions{AllowedIPs: []string{"not-cidr"}, PresharedKey: "super-secret-psk"})
	if err == nil || !strings.Contains(err.Error(), "invalid allowed ips") {
		t.Fatalf("err = %v", err)
	}
	if len(runner.argvs) != 0 {
		t.Fatalf("runtime was called: %#v", runner.argvs)
	}
}

func TestSetInterfaceConfigStreamsPrivateKeyAndAppliesCompleteInterface(t *testing.T) {
	runner := &fakeRunner{}
	facade := New(WithRuntime(container.New(container.WithRunner(runner))))
	const headerKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE="
	if err := facade.SetInterfaceConfig(context.Background(), InterfaceConfig{PrivateKey: "server-private", HeaderProtectionKey: headerKey, Address: "10.8.1.1/24", ListenPort: 51820, JC: 1, JMin: 2, JMax: 3, S1: 12, S2: 13, S3: 14, S4: 15, H1: "8", H2: "9", H3: "10", H4: "11", I1: "<r 12>", I2: "<r 13>", I3: "<r 14>", I4: "<r 15>", I5: "<r 16>"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.argvs) != 2 || !reflect.DeepEqual(runner.argvs[0][:2], []string{"sh", "-ceu"}) {
		t.Fatalf("unexpected argv: %#v", runner.argvs)
	}
	joined := strings.Join(runner.argvs[0], " ")
	for _, expected := range []string{"awg0", "listen-port 51820", "jc 1", "i5 <r 16>", "random-trailers off", "disable-cookies off"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("argv missing %q: %q", expected, joined)
		}
	}
	if strings.Contains(joined, "server-private") || strings.Contains(joined, headerKey) || string(runner.stdins[0]) != "server-private\n"+headerKey+"\n" {
		t.Fatalf("private key transport is unsafe: argv=%q stdin=%q", joined, runner.stdins[0])
	}
	script := runner.argvs[0][2]
	for _, expected := range []string{"/dev/shm/lazgate-awg-key-", "private-key \"$key_path\""} {
		if !strings.Contains(script, expected) {
			t.Fatalf("script missing %q:\n%s", expected, script)
		}
	}
	if !reflect.DeepEqual(runner.argvs[1], []string{"ip", "address", "replace", "10.8.1.1/24", "dev", "awg0"}) {
		t.Fatal("deployment must use a separate ip command")
	}
}

func TestNativeSystemdCommandsAndStatus(t *testing.T) {
	for _, service := range []string{"lab-awg", "lab-awg.service"} {
		runner := &fakeRunner{results: []container.Result{{Stdout: " Active \n"}}}
		facade := New(WithRuntime(container.New(container.WithRunner(runner))), WithServiceName(service), WithSudo(true))
		status, err := facade.Status(context.Background())
		if err != nil || status != "active" {
			t.Fatalf("status=%q err=%v", status, err)
		}
		if err := facade.Restart(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := facade.Logs(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := facade.Inspect(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := [][]string{
			{"sudo", "-n", "systemctl", "show", "--property=ActiveState", "--value", "lab-awg.service"},
			{"sudo", "-n", "systemctl", "restart", "lab-awg.service"},
			{"sudo", "-n", "journalctl", "--unit", "lab-awg.service", "--lines", "80", "--no-pager"},
			{"sudo", "-n", "systemctl", "show", "lab-awg.service"},
		}
		if !reflect.DeepEqual(runner.argvs, want) {
			t.Fatalf("argvs=%#v", runner.argvs)
		}
	}
}

func TestShowDumpExecutesLocalAWGBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "awg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n[ \"$*\" = 'show awg7 dump' ] || exit 1\nprintf '%s\\n' '"+goldenDump+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	facade := New(WithAWGBinaryPath(path), WithInterfaceName("awg7"))
	dump, err := facade.ShowDump(context.Background())
	if err != nil || len(dump.Peers) != 2 {
		t.Fatalf("dump=%+v err=%v", dump, err)
	}
}

func TestInterfaceConfigRejectsInvalidProtectionBeforeExecution(t *testing.T) {
	for _, config := range []InterfaceConfig{
		{PrivateKey: "private", S1: 12, S2: 12, S3: 12, S4: 12},
		{PrivateKey: "private", HeaderProtectionKey: "bad", S1: 12, S2: 12, S3: 12, S4: 12},
		{PrivateKey: "private", HeaderProtectionKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=", S1: 11, S2: 12, S3: 12, S4: 12},
	} {
		runner := &fakeRunner{}
		facade := New(WithRuntime(container.New(container.WithRunner(runner))))
		if err := facade.SetInterfaceConfig(context.Background(), config); err == nil || len(runner.argvs) != 0 {
			t.Fatalf("err=%v calls=%d", err, len(runner.argvs))
		}
	}
}

func TestSecretCommandsUseConfiguredAWGWithoutSudoShell(t *testing.T) {
	runner := &fakeRunner{}
	facade := New(WithRuntime(container.New(container.WithRunner(runner))), WithAWGBinaryPath("/usr/local/bin/awg"), WithSudo(true))
	if err := facade.SetPeer(context.Background(), "peer", PeerOptions{PresharedKey: "secret-psk"}); err != nil {
		t.Fatal(err)
	}
	if err := facade.SetInterfaceConfig(context.Background(), InterfaceConfig{
		PrivateKey: "private", HeaderProtectionKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAE=",
		Address: "10.8.1.1/24", ListenPort: 51820, S1: 12, S2: 12, S3: 12, S4: 12,
	}); err != nil {
		t.Fatal(err)
	}
	for _, argv := range runner.argvs {
		if reflect.DeepEqual(argv, []string{"sudo", "-n", "ip", "address", "replace", "10.8.1.1/24", "dev", "awg0"}) {
			continue
		}
		if !reflect.DeepEqual(argv[:2], []string{"sh", "-ceu"}) || !reflect.DeepEqual(argv[4:6], []string{"/usr/local/bin/awg", "true"}) {
			t.Fatalf("argv=%q", argv)
		}
		if !strings.Contains(argv[2], `sudo -n "$awg_binary" "$@"`) {
			t.Fatal("missing restricted awg elevation")
		}
	}
	args := runner.argvs[1]
	for i, arg := range args {
		if (arg == "i1" || arg == "i5") && (i+1 >= len(args) || args[i+1] != "") {
			t.Fatal("empty I parameters must explicitly clear runtime CPS")
		}
	}
}
