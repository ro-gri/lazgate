package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hysteriaproto "laz/generated/nodeproto/hysteria"
	"laz/internal/protocol/hy2config"
)

func TestEditServerConfigRejectsUnconfiguredReconciler(t *testing.T) {
	command := &hysteriaproto.EditServerConfig{NodeId: "node-1", Revision: 42, ServerConfig: &hysteriaproto.ServerConfig{}}
	result := New().EditServerConfig(context.Background(), command)

	if result.GetStatus() != "failed" || result.GetNodeId() != "node-1" || result.GetDesiredRevision() != 42 || result.GetAppliedRevision() != 0 || result.GetError() == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestApplyPreservesIntegrationAndRollsBackOnRestartFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "rollback"}[fail], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			original := []byte("listen: :443\nauth:\n  type: http\n  http:\n    url: http://127.0.0.1:28262/auth\ntrafficStats:\n  secret: private-stats\nacme:\n  domains: [old.example.com]\n  email: owner@example.com\n")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			r := New(Options{NodeID: "node-1", ConfigPath: path, ServiceName: "hysteria-server"})
			restarts := 0
			r.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if name != "sudo" {
					t.Fatal("unexpected command", name, args)
				}
				switch args[0] {
				case "cat":
					return os.ReadFile(args[1])
				case "cp":
					raw, e := os.ReadFile(args[len(args)-2])
					if e != nil {
						return nil, e
					}
					return nil, os.WriteFile(args[len(args)-1], raw, 0600)
				case "install":
					raw, e := os.ReadFile(args[len(args)-2])
					if e != nil {
						return nil, e
					}
					return nil, os.WriteFile(args[len(args)-1], raw, 0600)
				case "mv":
					return nil, os.Rename(args[len(args)-2], args[len(args)-1])
				case "rm":
					return nil, os.Remove(args[len(args)-1])
				case "systemctl":
					if args[1] == "restart" {
						restarts++
						if fail && restarts == 1 {
							return nil, errors.New("simulated restart failure")
						}
					}
					return nil, nil
				}
				t.Fatalf("unexpected command %s %v", name, args)
				return nil, nil
			}
			c := hy2config.Defaults()
			c.ListenPort = 8443
			raw, _ := json.Marshal(c)
			result := r.EditServerConfig(context.Background(), &hysteriaproto.EditServerConfig{NodeId: "node-1", Revision: 42, ServerConfig: &hysteriaproto.ServerConfig{EndpointHost: "new.example.com", SettingsJson: raw}})
			applied, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if fail {
				if result.Status != "failed" || string(applied) != string(original) || restarts != 2 {
					t.Fatalf("rollback failed: %+v %s", result, applied)
				}
			} else {
				if result.Status != "applied" || result.AppliedRevision != 42 {
					t.Fatalf("apply failed: %+v", result)
				}
				for _, value := range []string{"private-stats", "owner@example.com", "new.example.com", "127.0.0.1:28262/auth", "maxIdleTimeout: 30s", ":8443"} {
					if !strings.Contains(string(applied), value) {
						t.Fatalf("lost %s: %s", value, applied)
					}
				}
			}
			if _, err := os.Stat(path + ".lazgate-settings-previous"); !os.IsNotExist(err) {
				t.Fatal("rollback artifact remains")
			}
		})
	}
}

func TestEditServerConfigRejectsMissingCommand(t *testing.T) {
	result := New().EditServerConfig(context.Background(), nil)
	if result.GetStatus() != "rejected" || result.GetError() == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}
