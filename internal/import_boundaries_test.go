package internal_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestAgentServerImportBoundaries(t *testing.T) {
	out, err := exec.Command("go", "list", "-json", "laz/internal/agent/...", "laz/internal/legacy/agent/...", "laz/internal/server/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}
	decoder := json.NewDecoder(strings.NewReader(string(out)))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		for _, imported := range pkg.Imports {
			if isAgentPackage(pkg.ImportPath) && (imported == "laz/internal/server" || strings.HasPrefix(imported, "laz/internal/server/")) {
				t.Fatalf("agent package %s imports server package %s", pkg.ImportPath, imported)
			}
			if strings.HasPrefix(pkg.ImportPath, "laz/internal/server") && isAgentPackage(imported) {
				t.Fatalf("server package %s imports agent package %s", pkg.ImportPath, imported)
			}
		}
	}
}

func isAgentPackage(path string) bool {
	return strings.HasPrefix(path, "laz/internal/agent/") || strings.HasPrefix(path, "laz/internal/legacy/agent/")
}

func TestArtifactTransitiveDependencies(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		forbidden     []string
	}{
		{name: "server", command: "laz/cmd/laz", forbidden: []string{"laz/internal/agent", "laz/internal/legacy/agent"}},
		{name: "hysteria-agent", command: "laz/cmd/lazgate-hysteria-agent", forbidden: []string{"laz/internal/server", "laz/internal/legacy/server", "laz/internal/agent/amneziawg", "laz/internal/legacy/agent/amneziawg", "laz/generated/nodeproto/amnezia"}},
		{name: "amnezia-agent", command: "laz/cmd/lazgate-amnezia-agent", forbidden: []string{"laz/internal/server", "laz/internal/legacy/server", "laz/internal/agent/hysteria2", "laz/internal/legacy/agent/hysteria2", "laz/generated/nodeproto/hysteria"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", tc.command).CombinedOutput()
			if err != nil {
				t.Fatalf("dependency resolution failed: %v\n%s", err, out)
			}
			for _, dependency := range strings.Fields(string(out)) {
				for _, forbidden := range tc.forbidden {
					if dependency == forbidden || strings.HasPrefix(dependency, forbidden+"/") {
						t.Errorf("%s transitively depends on forbidden package %s", tc.command, dependency)
					}
				}
			}
		})
	}
}
