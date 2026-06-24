package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"laz/internal/protocol/hy2files"
)

type Manager struct {
	Root string
	Run  func(context.Context, string, ...string) ([]byte, error)
}

func (m Manager) safePath(target string) error {
	relative, err := filepath.Rel(m.Root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("invalid managed file path")
	}
	current := m.Root
	for _, part := range append([]string{""}, strings.Split(relative, string(filepath.Separator))...) {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not allowed in the managed directory")
		}
	}
	return nil
}

func (m Manager) Stage(ctx context.Context, id string, archive []byte) (string, error) {
	files, err := hy2files.Verify(id, archive)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	if err := hy2files.RequireIndex(paths); err != nil {
		return "", err
	}
	target := filepath.Join(m.Root, id)
	marker := target + ".ready"
	if err = m.safePath(target); err != nil {
		return "", err
	}
	if err = m.safePath(marker); err != nil {
		return "", err
	}
	if raw, e := os.ReadFile(marker); e == nil && string(raw) == id {
		if info, e := os.Stat(target); e == nil && info.IsDir() {
			return target, nil
		}
	}
	temporary, err := os.MkdirTemp("", "laz-h2-files-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	if _, err = m.Run(ctx, "sudo", "install", "-d", "-m", "755", m.Root, target); err != nil {
		return "", errors.New("cannot create managed masquerade directory")
	}
	for index, file := range files {
		destination := filepath.Join(target, filepath.FromSlash(file.Path))
		if err = m.safePath(destination); err != nil {
			return "", err
		}
		if _, err = m.Run(ctx, "sudo", "install", "-d", "-m", "755", filepath.Dir(destination)); err != nil {
			return "", errors.New("cannot create masquerade subdirectory")
		}
		source := filepath.Join(temporary, fmt.Sprint(index))
		if err = os.WriteFile(source, file.Data, 0600); err != nil {
			return "", err
		}
		if _, err = m.Run(ctx, "sudo", "install", "-m", "644", source, destination); err != nil {
			return "", errors.New("cannot install masquerade file")
		}
	}
	source := filepath.Join(temporary, "ready")
	if err = os.WriteFile(source, []byte(id), 0600); err != nil {
		return "", err
	}
	if _, err = m.Run(ctx, "sudo", "install", "-m", "644", source, marker); err != nil {
		return "", errors.New("cannot finalize masquerade file set")
	}
	return target, nil
}

// Keep the active set and the previous one; neither is removed before successful runtime verification.
func (m Manager) Cleanup(ctx context.Context, keepPaths ...string) error {
	keep := map[string]bool{}
	for _, p := range keepPaths {
		if filepath.Dir(p) == m.Root && hy2files.ValidID(filepath.Base(p)) {
			keep[filepath.Base(p)] = true
		}
	}
	entries, err := os.ReadDir(m.Root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !hy2files.ValidID(entry.Name()) || keep[entry.Name()] {
			continue
		}
		target := filepath.Join(m.Root, entry.Name())
		if err = m.safePath(target); err != nil {
			return err
		}
		if _, err = m.Run(ctx, "sudo", "rm", "-rf", target, target+".ready"); err != nil {
			return errors.New("cannot remove unused masquerade file set")
		}
	}
	return nil
}
