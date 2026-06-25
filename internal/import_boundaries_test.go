package internal_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestAgentServerImportBoundaries(t *testing.T) {
	out, err := exec.Command("go", "list", "-json", "laz/internal/agent/...", "laz/internal/server/...").CombinedOutput()
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
			if strings.HasPrefix(pkg.ImportPath, "laz/internal/agent") && strings.HasPrefix(imported, "laz/internal/server") {
				t.Fatalf("agent package %s imports server package %s", pkg.ImportPath, imported)
			}
			if strings.HasPrefix(pkg.ImportPath, "laz/internal/server") && strings.HasPrefix(imported, "laz/internal/agent") {
				t.Fatalf("server package %s imports agent package %s", pkg.ImportPath, imported)
			}
		}
	}
}
