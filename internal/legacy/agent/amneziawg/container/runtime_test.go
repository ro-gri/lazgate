package container

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	result Result
	err    error
	seen   []string
	stdin  []byte
	limit  int
	wait   time.Duration
}

func (f *fakeRunner) Run(ctx context.Context, argv []string, stdin []byte, limit int) (Result, error) {
	f.seen = append([]string(nil), argv...)
	f.stdin = append([]byte(nil), stdin...)
	f.limit = limit
	if f.wait > 0 {
		select {
		case <-time.After(f.wait):
		case <-ctx.Done():
			return f.result, ctx.Err()
		}
	}
	return f.result, f.err
}

func TestRuntimeRunUsesArgvAndRedactsFailures(t *testing.T) {
	runner := &fakeRunner{
		result: Result{Stdout: "PrivateKey = secret-key\n", Stderr: "failed psk-value"},
		err:    errors.New("exit status 1"),
	}
	rt := New(WithRunner(runner), WithOutputLimit(123), WithSecrets("secret-key"))
	_, err := rt.Run(context.Background(), []string{"awg", "set", "awg0", "peer", "pub", "preshared-key", "psk-value"}, nil, "psk-value")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "secret-key") || strings.Contains(msg, "psk-value") {
		t.Fatalf("error leaked secret: %s", msg)
	}
	if !strings.Contains(msg, "<redacted>") {
		t.Fatalf("expected redacted marker in %q", msg)
	}
	if got := strings.Join(runner.seen, " "); strings.Contains(got, "sh -c") {
		t.Fatalf("unexpected shell command: %s", got)
	}
	if runner.limit != 123 {
		t.Fatalf("limit = %d, want 123", runner.limit)
	}
}

func TestRuntimeRunTimeout(t *testing.T) {
	runner := &fakeRunner{wait: 50 * time.Millisecond}
	rt := New(WithRunner(runner), WithTimeout(5*time.Millisecond))
	_, err := rt.Run(context.Background(), []string{"systemctl", "show", "awg-quick@awg0.service"}, nil)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestLimitBufferBoundsOutput(t *testing.T) {
	var buf limitBuffer
	buf.limit = 4
	if _, err := buf.Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "abcd...<truncated>" {
		t.Fatalf("got %q", got)
	}
}

func TestRedactsHeaderProtectionKey(t *testing.T) {
	for _, key := range []string{"HeaderProtectionKey", "header_protection_key", "header-protection-key", "header protection key"} {
		if got := RedactString(key + " = sensitive-value"); strings.Contains(got, "sensitive-value") {
			t.Fatalf("not redacted: %s", got)
		}
	}
}

func TestSecretOutputIsRawOnlyOnSuccess(t *testing.T) {
	runner := &fakeRunner{result: Result{Stdout: "PrivateKey = secret\n"}}
	rt := New(WithRunner(runner))
	result, err := rt.RunSecretOutput(context.Background(), []string{"awg", "showconf", "awg0"})
	if err != nil || result.Stdout != runner.result.Stdout {
		t.Fatal("successful secret output was changed")
	}
	runner.err = errors.New("command failed")
	result, err = rt.RunSecretOutput(context.Background(), []string{"awg", "showconf", "awg0"})
	if err == nil || strings.Contains(result.Stdout, "secret") || strings.Contains(err.Error(), "secret") {
		t.Fatal("failure leaked raw secret output")
	}
}
