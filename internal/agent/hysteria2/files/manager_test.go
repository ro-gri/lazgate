package files

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"laz/internal/protocol/hy2files"
)

func localRun(_ context.Context, _ string, args ...string) ([]byte, error) {
	switch args[0] {
	case "install":
		if args[1] == "-d" {
			for _, dir := range args[4:] {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}
		raw, err := os.ReadFile(args[len(args)-2])
		if err != nil {
			return nil, err
		}
		return nil, os.WriteFile(args[len(args)-1], raw, 0644)
	case "rm":
		for _, name := range args[2:] {
			if err := os.RemoveAll(name); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	return nil, errors.New("unexpected command")
}
func TestStageAndKeepPreviousSet(t *testing.T) {
	ctx := context.Background()
	m := Manager{Root: filepath.Join(t.TempDir(), "files"), Run: localRun}
	paths := []string{}
	for _, value := range []string{"one", "two", "three"} {
		b, err := hy2files.Build([]hy2files.File{{Path: "css/site.css", Data: []byte(value)}, {Path: "index.html", Data: []byte(value)}})
		if err != nil {
			t.Fatal(err)
		}
		dir, err := m.Stage(ctx, b.Manifest.ID, b.Archive)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, dir)
		raw, err := os.ReadFile(filepath.Join(dir, "css/site.css"))
		if err != nil || string(raw) != value {
			t.Fatal("file not installed")
		}
		if _, err = m.Stage(ctx, b.Manifest.ID, b.Archive); err != nil {
			t.Fatal("idempotent stage failed", err)
		}
	}
	if err := m.Cleanup(ctx, paths[1], paths[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatal("unused set retained")
	}
	for _, p := range paths[1:] {
		if _, err := os.Stat(p); err != nil {
			t.Fatal("active or previous set removed")
		}
	}
}
func TestRejectFilesystemSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	b, _ := hy2files.Build([]hy2files.File{{Path: "index.html", Data: []byte("safe")}})
	if err := os.Symlink(outside, filepath.Join(root, b.Manifest.ID)); err != nil {
		t.Fatal(err)
	}
	m := Manager{Root: root, Run: localRun}
	if _, err := m.Stage(context.Background(), b.Manifest.ID, b.Archive); err == nil {
		t.Fatal("symlink accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) > 0 {
		t.Fatal("wrote outside managed root")
	}
}

func TestRejectApplyingDraftWithoutRootIndex(t *testing.T) {
	b, err := hy2files.Build([]hy2files.File{{Path: "site/index.html", Data: []byte("draft")}})
	if err != nil {
		t.Fatal(err)
	}
	m := Manager{Root: t.TempDir(), Run: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("invalid set reached filesystem installation")
		return nil, nil
	}}
	if _, err := m.Stage(context.Background(), b.Manifest.ID, b.Archive); !errors.Is(err, hy2files.ErrMissingIndex) {
		t.Fatalf("missing root index accepted: %v", err)
	}
}
