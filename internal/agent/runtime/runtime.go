package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"laz/internal/agent/hysteria2/statsapi"
	"laz/internal/nodeproto"
)

type Executor struct {
	serviceName string
	stats       *statsapi.Client
	logLines    int
}

func New(serviceName string, stats *statsapi.Client, logLines int) *Executor {
	return &Executor{serviceName: serviceName, stats: stats, logLines: logLines}
}

func (e *Executor) Execute(ctx context.Context, cmd *nodeproto.RuntimeCommand) *nodeproto.RuntimeCommandResult {
	if cmd == nil {
		return &nodeproto.RuntimeCommandResult{Status: "error", Error: "empty command"}
	}
	if cmd.ExpiresMs > 0 && time.Now().UnixMilli() > cmd.ExpiresMs {
		return &nodeproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "skipped", Error: "command expired"}
	}
	var msg string
	var err error
	switch cmd.Type {
	case "GetStatus":
		msg, err = e.systemctl(ctx, "is-active")
	case "RestartHysteria":
		msg, err = e.systemctl(ctx, "restart")
	case "StartHysteria":
		msg, err = e.systemctl(ctx, "start")
	case "StopHysteria":
		msg, err = e.systemctl(ctx, "stop")
	case "CollectLogs":
		msg, err = e.logs(ctx)
	case "DumpStreams":
		msg, err = e.stats.DumpStreams(ctx)
	case "KickClient":
		credentialID := cmd.Payload["credential_id"]
		if credentialID == "" {
			err = errors.New("credential_id is required")
		} else {
			err = e.stats.Kick(ctx, credentialID)
			msg = "kick requested"
		}
	case "ApplyHysteriaConfig":
		msg, err = e.applyConfig(ctx, cmd.Payload["config"])
	default:
		return &nodeproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "unsupported", Error: "unsupported command"}
	}
	if err != nil {
		return &nodeproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "error", Error: sanitize(err.Error())}
	}
	return &nodeproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "ok", Message: sanitize(msg)}
}

func (e *Executor) systemctl(ctx context.Context, action string) (string, error) {
	out, err := exec.CommandContext(ctx, "sudo", "systemctl", action, e.serviceName).CombinedOutput()
	return string(out), err
}

func (e *Executor) logs(ctx context.Context) (string, error) {
	lines := "80"
	if e.logLines > 0 {
		lines = strconv.Itoa(e.logLines)
	}
	out, err := exec.CommandContext(ctx, "sudo", "journalctl", "-u", e.serviceName, "-n", lines, "--no-pager").CombinedOutput()
	return string(out), err
}

func (e *Executor) applyConfig(ctx context.Context, config string) (string, error) {
	config = strings.TrimSpace(config)
	if config == "" {
		return "", errors.New("config is required")
	}
	if strings.Contains(config, "\x00") || len(config) > 1<<20 {
		return "", errors.New("invalid config")
	}
	tmp, err := os.CreateTemp("", "lazgate-hysteria-*.yaml")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(config + "\n"); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	target := "/etc/hysteria/config.yaml"
	backup := filepath.Join("/etc/hysteria", "config.lazgate-backup-"+strconv.FormatInt(time.Now().Unix(), 10)+".yaml")
	if out, err := exec.CommandContext(ctx, "sudo", "cp", "-p", target, backup).CombinedOutput(); err != nil {
		return "", errors.New(sanitize(string(out)))
	}
	if out, err := exec.CommandContext(ctx, "sudo", "install", "-m", "600", tmpPath, target).CombinedOutput(); err != nil {
		return "", errors.New(sanitize(string(out)))
	}
	if _, err := e.systemctl(ctx, "restart"); err != nil {
		_, _ = exec.CommandContext(ctx, "sudo", "cp", "-p", backup, target).CombinedOutput()
		_, _ = e.systemctl(ctx, "restart")
		return "", err
	}
	if _, err := e.systemctl(ctx, "is-active"); err != nil {
		_, _ = exec.CommandContext(ctx, "sudo", "cp", "-p", backup, target).CombinedOutput()
		_, _ = e.systemctl(ctx, "restart")
		return "", err
	}
	return "config applied; backup: " + filepath.Base(backup), nil
}

func sanitize(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 8000 {
		value = value[:8000] + "...<truncated>"
	}
	return value
}
