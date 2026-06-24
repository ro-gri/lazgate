package runtime

import (
	"context"
	"strings"

	agentcommon "laz/internal/agent/common"
)

type RuntimeCollector struct {
	tools interface {
		Status(context.Context) (string, error)
	}
}

func NewRuntimeCollector(tools interface {
	Status(context.Context) (string, error)
}) *RuntimeCollector {
	return &RuntimeCollector{tools: tools}
}

func (c *RuntimeCollector) Collect(ctx context.Context) (agentcommon.CollectionResult[agentcommon.RuntimeSnapshot], error) {
	if c == nil {
		return agentcommon.CollectionResult[agentcommon.RuntimeSnapshot]{
			Value:  agentcommon.RuntimeSnapshot{Status: "unknown"},
			Issues: []agentcommon.CollectionIssue{{Code: "collector_unavailable", Message: "AmneziaWG runtime collector is nil"}},
		}, nil
	}
	report := agentcommon.CollectionResult[agentcommon.RuntimeSnapshot]{Value: agentcommon.RuntimeSnapshot{Status: "unknown"}}
	if c.tools == nil {
		report.Issues = append(report.Issues, agentcommon.CollectionIssue{Code: "tools_unavailable", Message: "AmneziaWG tools are not configured"})
		return report, nil
	}
	if status, err := c.tools.Status(ctx); err != nil {
		report.Issues = append(report.Issues, agentcommon.CollectionIssue{Code: "status_unavailable", Message: sanitizeMessage(err.Error())})
	} else {
		report.Value.Status = sanitizeMessage(status)
	}
	return report, nil
}

func sanitizeMessage(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	sensitive := []string{"privatekey", "private_key", "headerprotectionkey", "header_protection_key", "header-protection-key", "header protection key", "token", "secret", "password", "passwd", "authorization", "env"}
	for _, marker := range sensitive {
		if strings.Contains(lower, marker) {
			return "<redacted>"
		}
	}
	if len(value) > 8000 {
		value = value[:8000] + "...<truncated>"
	}
	return value
}
