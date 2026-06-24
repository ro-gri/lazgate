package container

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

const (
	DefaultTimeout     = 10 * time.Second
	DefaultOutputLimit = 64 * 1024
)

var ErrTimeout = errors.New("command timed out")

// Runner executes a process without going through a shell.
type Runner interface {
	Run(ctx context.Context, argv []string, stdin []byte, limit int) (Result, error)
}

// Result is a bounded command execution result safe to return after redaction.
type Result struct {
	Argv     []string
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runtime wraps host command execution with deadlines, bounded output, and redaction.
type Runtime struct {
	runner      Runner
	timeout     time.Duration
	outputLimit int
	secrets     []string
}

// Option customizes Runtime.
type Option func(*Runtime)

func WithRunner(r Runner) Option {
	return func(rt *Runtime) {
		if r != nil {
			rt.runner = r
		}
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(rt *Runtime) {
		if timeout > 0 {
			rt.timeout = timeout
		}
	}
}

func WithOutputLimit(limit int) Option {
	return func(rt *Runtime) {
		if limit > 0 {
			rt.outputLimit = limit
		}
	}
}

func WithSecrets(secrets ...string) Option {
	return func(rt *Runtime) {
		rt.secrets = append(rt.secrets, secrets...)
	}
}

func New(options ...Option) *Runtime {
	rt := &Runtime{
		runner:      ExecRunner{},
		timeout:     DefaultTimeout,
		outputLimit: DefaultOutputLimit,
	}
	for _, option := range options {
		option(rt)
	}
	return rt
}

func (rt *Runtime) Run(ctx context.Context, argv []string, stdin []byte, extraSecrets ...string) (Result, error) {
	return rt.run(ctx, argv, stdin, false, extraSecrets...)
}

// RunSecretOutput returns unredacted stdout only on success for local secret
// persistence. Callers must never log it. Errors always use redacted output.
func (rt *Runtime) RunSecretOutput(ctx context.Context, argv []string) (Result, error) {
	return rt.run(ctx, argv, nil, true)
}

func (rt *Runtime) run(ctx context.Context, argv []string, stdin []byte, secretOutput bool, extraSecrets ...string) (Result, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Result{}, errors.New("argv is required")
	}
	args := append([]string(nil), argv...)
	secrets := append([]string(nil), rt.secrets...)
	secrets = append(secrets, extraSecrets...)
	ctx, cancel := context.WithTimeout(ctx, rt.timeout)
	defer cancel()
	res, err := rt.runner.Run(ctx, args, stdin, rt.outputLimit)
	res.Argv = append([]string(nil), res.Argv...)
	if len(res.Argv) == 0 {
		res.Argv = args
	}
	stdout := res.Stdout
	res = RedactResult(res, secrets...)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return res, fmt.Errorf("%w: %s", ErrTimeout, FormatArgv(RedactArgs(args, secrets...)))
	}
	if err != nil {
		return res, redactError(fmt.Errorf("%w: %s", err, formatResult(res)), secrets...)
	}
	if secretOutput {
		if strings.Contains(stdout, "...<truncated>") {
			return Result{}, errors.New("secret command output exceeded limit")
		}
		res.Stdout = stdout
	}
	return res, nil
}

// ExecRunner executes argv directly with exec.CommandContext.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string, stdin []byte, limit int) (Result, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr limitBuffer
	stdout.limit = limit
	stderr.limit = limit
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{Argv: append([]string(nil), argv...), Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			res.ExitCode = exit.ExitCode()
		}
	}
	return res, err
}

type limitBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.truncated = true
		_, _ = b.buf.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *limitBuffer) String() string {
	out := b.buf.String()
	if b.truncated {
		out += "...<truncated>"
	}
	return out
}

func FormatArgv(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, quoteArg(arg))
	}
	return strings.Join(quoted, " ")
}

func RedactResult(res Result, secrets ...string) Result {
	res.Argv = RedactArgs(res.Argv, secrets...)
	res.Stdout = RedactString(res.Stdout, secrets...)
	res.Stderr = RedactString(res.Stderr, secrets...)
	return res
}

func RedactArgs(argv []string, secrets ...string) []string {
	out := append([]string(nil), argv...)
	for i, arg := range out {
		out[i] = RedactString(arg, secrets...)
	}
	return out
}

func RedactString(value string, secrets ...string) string {
	out := value
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret != "" {
			out = strings.ReplaceAll(out, secret, "<redacted>")
		}
	}
	out = redactConfigValues(out)
	return out
}

func redactError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	return errors.New(RedactString(err.Error(), secrets...))
}

func formatResult(res Result) string {
	parts := []string{"argv=" + FormatArgv(res.Argv)}
	if res.Stdout != "" {
		parts = append(parts, "stdout="+res.Stdout)
	}
	if res.Stderr != "" {
		parts = append(parts, "stderr="+res.Stderr)
	}
	return strings.Join(parts, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	for _, r := range arg {
		if unicode.IsSpace(r) || strings.ContainsRune("'\"\\$`", r) {
			return fmt.Sprintf("%q", arg)
		}
	}
	return arg
}

func redactConfigValues(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		for _, key := range []string{"privatekey", "presharedkey", "pre-shared key", "psk", "headerprotectionkey", "header_protection_key", "header-protection-key", "header protection key"} {
			if strings.HasPrefix(lower, key) {
				if idx := strings.Index(line, "="); idx >= 0 {
					lines[i] = line[:idx+1] + " <redacted>"
				} else if fields := strings.Fields(line); len(fields) > 1 {
					lines[i] = fields[0] + " <redacted>"
				}
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}
