package provisioning

import (
	"laz/internal/protocol/hy2files"
	"strings"
	"testing"
)

func TestHysteriaAgentUnitSupportsManagedFiles(t *testing.T) {
	unit := lazgateAgentUnit()
	if !strings.Contains(unit, "NoNewPrivileges=false") || !strings.Contains(unit, "ReadWritePaths=/var/lib/lazgate-agent /etc/hysteria "+hy2files.ManagedDirectory) {
		t.Fatal("agent sandbox blocks privileged file/runtime operations")
	}
	script := renderHysteriaAgentInstallScript(agentInstallScriptInput{})
	if !strings.Contains(script, "sudo install -d -o root -g root -m 755 "+shellQuote(hy2files.ManagedDirectory)) {
		t.Fatal("managed root is not created before systemd starts")
	}
}
