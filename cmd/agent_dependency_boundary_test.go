package cmd_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSplitAgentDependencyBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pkg       string
		forbidden []string
	}{
		{
			name: "hysteria agent excludes amnezia packages",
			pkg:  "./cmd/lazgate-hysteria-agent",
			forbidden: []string{
				"laz/internal/agent/amneziawg",
				"laz/internal/legacy/agent/amneziawg",
				"laz/internal/server/integrations/nativeawg/cps",
				"github.com/miekg/dns",
				"github.com/pion/stun/v3",
				"github.com/quic-go/quic-go",
			},
		},
		{
			name: "amnezia agent excludes hysteria runtime packages",
			pkg:  "./cmd/lazgate-amnezia-agent",
			forbidden: []string{
				"laz/internal/agent/hysteria2",
				"laz/internal/legacy/agent/hysteria2/auth",
				"laz/internal/legacy/agent/hysteria2",
				"laz/internal/legacy/agent/hysteria2/runtime",
				"laz/internal/server/integrations/nativeawg/cps",
				"github.com/miekg/dns",
				"github.com/pion/stun/v3",
				"github.com/quic-go/quic-go",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("go", "list", "-deps", tt.pkg)
			cmd.Dir = ".."
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go list -deps %s failed: %v\n%s", tt.pkg, err, out)
			}

			deps := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, dep := range deps {
				for _, forbidden := range tt.forbidden {
					if dep == forbidden || strings.HasPrefix(dep, forbidden+"/") {
						t.Fatalf("%s dependency graph contains forbidden package %q", tt.pkg, dep)
					}
				}
			}
		})
	}
}
