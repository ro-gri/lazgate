package runtime

import (
	"context"
	"errors"
	"testing"
)

func TestRuntimeCollectorReportsServiceStatus(t *testing.T) {
	collector := NewRuntimeCollector("hysteria-server")
	collector.status = func(_ context.Context, serviceName string) (string, error) {
		if serviceName != "hysteria-server" {
			t.Fatalf("service name = %q", serviceName)
		}
		return "active\n", nil
	}

	report, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if report.Value.Status != "active" || len(report.Issues) != 0 {
		t.Fatalf("unexpected runtime report: %+v", report)
	}
}

func TestRuntimeCollectorReportsStatusFailure(t *testing.T) {
	collector := NewRuntimeCollector("hysteria-server")
	collector.status = func(context.Context, string) (string, error) {
		return "", errors.New("systemd unavailable")
	}

	report, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if report.Value.Status != "unknown" || len(report.Issues) != 1 || report.Issues[0].Code != "status_unavailable" {
		t.Fatalf("unexpected runtime report: %+v", report)
	}
}
