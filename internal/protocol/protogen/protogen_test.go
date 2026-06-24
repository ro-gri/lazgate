package protogen

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedAgentProtocolFilesAreCurrent(t *testing.T) {
	repo := findRepoRoot(t)
	tests := []struct {
		name     string
		specPath string
		protoOut string
		goOut    string
	}{
		{name: "hysteria", specPath: "internal/legacy/agent/hysteria2/syncproto/hysteria.json", protoOut: "generated/proto/hysteria/agent.proto", goOut: "generated/nodeproto/hysteria/manifest_builder.go"},
		{name: "amnezia", specPath: "internal/legacy/agent/amneziawg/syncproto/amnezia.json", protoOut: "generated/proto/amnezia/agent.proto", goOut: "generated/nodeproto/amnezia/manifest_builder.go"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := LoadSpec(filepath.Join(repo, tc.specPath))
			if err != nil {
				t.Fatal(err)
			}
			assertGeneratedFile(t, repo, tc.protoOut, spec, Generate)
			assertGeneratedFile(t, repo, tc.goOut, spec, GenerateManifestBuilder)
		})
	}
}

func TestTemplatesAreEditableSources(t *testing.T) {
	entries, err := templates.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		raw, err := templates.ReadFile(filepath.Join("templates", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("Code generated")) {
			t.Errorf("template %s must not contain the generated-file marker", entry.Name())
		}
	}
}

func assertGeneratedFile(t *testing.T, repo, outputPath string, spec Spec, generate func(Spec) ([]byte, error)) {
	t.Helper()
	got, err := generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(repo, outputPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is stale; run make generate-proto", outputPath)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found")
		}
		dir = next
	}
}
