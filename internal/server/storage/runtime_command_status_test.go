package store

import (
	"laz/internal/server/model"
	"strings"
	"testing"
)

func TestRuntimeCommandStatus(t *testing.T) {
	st := openTargetSQLite(t)
	node := mustCreateNode(t, st, model.NodeTypeNativeHy2, "commands")
	instance, err := st.CreateNodeInstance(model.NodeInstance{NodeID: node.ID, Host: "192.0.2.12"})
	if err != nil {
		t.Fatal(err)
	}
	installation, err := st.CreateAgentInstallation(model.AgentInstallation{NodeInstanceID: instance.ID, AgentType: "hysteria2", CertificateFingerprintSHA256: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	command, err := st.CreateRuntimeCommand(model.RuntimeCommand{AgentInstallationID: installation.ID, Type: "GetStatus"})
	if err != nil || command.Status != model.RuntimeCommandStatusActive {
		t.Fatalf("new command: %+v %v", command, err)
	}
	if err := st.CompleteRuntimeCommandForInstallation(installation.ID, command.ID, model.RuntimeCommandStatusCompleted, "done", ""); err != nil {
		t.Fatal(err)
	}
	commands := st.ListRuntimeCommands(installation.ID, 10)
	if len(commands) != 1 || commands[0].Status != model.RuntimeCommandStatusCompleted {
		t.Fatalf("completed commands: %+v", commands)
	}
	if pending := st.ListPendingRuntimeCommands(installation.ID); len(pending) != 0 {
		t.Fatalf("completed commands pending: %+v", pending)
	}
	if err := st.CompleteRuntimeCommandForInstallation(installation.ID, command.ID, model.RuntimeCommandStatus("deleted"), "", ""); err == nil {
		t.Fatal("legacy completion status accepted")
	}
	account := mustCreateAccount(t, st, "command-status")
	if _, err := st.UpdateAccountStatus(account.ID, model.AccountStatus("completed")); err == nil {
		t.Fatal("command status accepted for account")
	}
}
