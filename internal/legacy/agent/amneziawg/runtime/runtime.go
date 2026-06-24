package runtime

import (
	"context"
	commonproto "laz/generated/nodeproto/agentcommon"
	"strings"
	"time"
)

type Tools interface {
	Status(context.Context) (string, error)
	Logs(context.Context) (string, error)
	Restart(context.Context) error
}

type Executor struct {
	tools Tools
}

func New(tools Tools) *Executor {
	return &Executor{tools: tools}
}

func (e *Executor) Execute(ctx context.Context, cmd *commonproto.RuntimeCommand) *commonproto.RuntimeCommandResult {
	if cmd == nil {
		return &commonproto.RuntimeCommandResult{Status: "error", Error: "empty command"}
	}
	if cmd.ExpiresMs > 0 && time.Now().UnixMilli() > cmd.ExpiresMs {
		return &commonproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "skipped", Error: "command expired"}
	}
	if e == nil || e.tools == nil {
		return &commonproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "unsupported", Error: "AmneziaWG tools are not configured"}
	}
	var msg string
	var err error
	switch cmd.Type {
	case "GetAmneziaWGStatus", "GetAWGStatus":
		msg, err = e.tools.Status(ctx)
	case "CollectAmneziaWGLogs", "CollectAWGLogs":
		msg, err = e.tools.Logs(ctx)
	case "RestartAmneziaWG", "RestartAWG":
		err = e.tools.Restart(ctx)
		if err == nil {
			msg = "AmneziaWG service restarted"
		}
	default:
		return &commonproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "unsupported", Error: "unsupported AmneziaWG command"}
	}
	if err != nil {
		return &commonproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "error", Error: sanitize(err.Error())}
	}
	return &commonproto.RuntimeCommandResult{CommandId: cmd.Id, Status: "ok", Message: sanitize(msg)}
}

func sanitize(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	for _, marker := range []string{"privatekey", "private_key", "presharedkey", "headerprotectionkey", "header_protection_key", "header-protection-key", "header protection key", "token", "secret", "password", "authorization"} {
		if strings.Contains(lower, marker) {
			return "<redacted>"
		}
	}
	if len(value) > 8000 {
		value = value[:8000] + "...<truncated>"
	}
	return value
}
