package runtime

import (
	"context"
	"os/exec"
	"strings"

	agentcommon "laz/internal/agent/common"
)

type RuntimeCollector struct {
	serviceName string
	status      func(context.Context, string) (string, error)
}

func NewRuntimeCollector(serviceName string) *RuntimeCollector {
	return &RuntimeCollector{
		serviceName: serviceName,
		status:      systemdServiceStatus,
	}
}

func (c *RuntimeCollector) Collect(ctx context.Context) (agentcommon.CollectionResult[agentcommon.RuntimeSnapshot], error) {
	if c == nil {
		return agentcommon.CollectionResult[agentcommon.RuntimeSnapshot]{
			Value:  agentcommon.RuntimeSnapshot{Status: "unknown"},
			Issues: []agentcommon.CollectionIssue{{Code: "collector_unavailable", Message: "Hysteria runtime collector is nil"}},
		}, nil
	}
	report := agentcommon.CollectionResult[agentcommon.RuntimeSnapshot]{Value: agentcommon.RuntimeSnapshot{Status: "unknown"}}
	status, err := c.status(ctx, c.serviceName)
	if err != nil {
		report.Issues = append(report.Issues, agentcommon.CollectionIssue{Code: "status_unavailable", Message: sanitize(err.Error())})
		return report, nil
	}
	report.Value.Status = strings.TrimSpace(status)
	if report.Value.Status == "" {
		report.Value.Status = "unknown"
		report.Issues = append(report.Issues, agentcommon.CollectionIssue{Code: "status_empty", Message: "systemd returned an empty service status"})
	}
	return report, nil
}

func systemdServiceStatus(ctx context.Context, serviceName string) (string, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", serviceName).Output()
	return string(out), err
}
