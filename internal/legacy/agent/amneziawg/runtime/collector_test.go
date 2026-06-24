package runtime

import (
	"context"
	"errors"
	"testing"

	"laz/internal/legacy/agent/amneziawg/tools"
)

type fakeTools struct {
	dump      tools.InterfaceDump
	status    string
	dumpErr   error
	statusErr error
}

func (f fakeTools) ShowDump(context.Context) (tools.InterfaceDump, error) {
	return f.dump, f.dumpErr
}

func (f fakeTools) Status(context.Context) (string, error) {
	return f.status, f.statusErr
}

func TestRuntimeCollectorReportsStatus(t *testing.T) {
	collector := NewRuntimeCollector(fakeTools{
		status: " active \n",
	})

	report, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if report.Value.Status != "active" || len(report.Issues) != 0 {
		t.Fatalf("runtime report = %+v, want active status", report)
	}
}

func TestRuntimeCollectorSanitizesSensitiveStatusError(t *testing.T) {
	collector := NewRuntimeCollector(fakeTools{
		statusErr: errors.New("PrivateKey=secret status failed"),
	})
	report, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(report.Issues) != 1 || report.Issues[0].Message != "<redacted>" {
		t.Fatalf("issues = %+v, want one redacted status issue", report.Issues)
	}
}

func TestCollectorsReturnSafeErrorsWhenNil(t *testing.T) {
	var runtime *RuntimeCollector
	if report, _ := runtime.Collect(context.Background()); len(report.Issues) != 1 || report.Issues[0].Code != "collector_unavailable" {
		t.Fatalf("runtime nil report = %+v, want collector_unavailable issue", report)
	}
}
