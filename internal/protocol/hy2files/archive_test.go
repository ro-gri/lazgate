package hy2files

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"testing"
)

func TestBuildDeterministicAndVerified(t *testing.T) {
	files := []File{{Path: "css/style.css", Data: []byte("body{}")}, {Path: "index.html", Data: []byte("hello")}}
	a, err := Build(files)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build([]File{files[1], files[0]})
	if err != nil {
		t.Fatal(err)
	}
	if a.Manifest.ID != b.Manifest.ID || a.Manifest.Size != 11 || len(a.Manifest.Files) != 2 {
		t.Fatalf("bad manifest: %+v", a.Manifest)
	}
	restored, err := Verify(a.Manifest.ID, a.Archive)
	if err != nil || len(restored) != 2 {
		t.Fatalf("verify: %v", err)
	}
	corrupted := bytes.Clone(a.Archive)
	corrupted[0] ^= 1
	if _, err = Verify(a.Manifest.ID, corrupted); err == nil {
		t.Fatal("accepted corrupted archive")
	}
}
func TestRejectUnsafeAndConflictingPaths(t *testing.T) {
	for _, name := range []string{"../index.html", "a/../../b", "/etc/passwd", "C:/index.html", "a\\b", "a//b", ".", "a/./b", "a\x00b", string([]byte{0xff})} {
		t.Run(name, func(t *testing.T) {
			if _, err := Build([]File{{Path: name, Data: []byte("bad")}}); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	for _, files := range [][]File{{{Path: "a"}, {Path: "a"}}, {{Path: "a"}, {Path: "a/b"}}} {
		if _, err := Build(files); err == nil {
			t.Fatal("conflicting files accepted")
		}
	}
}
func TestRejectSymlinkAndOversizedExpandedZIP(t *testing.T) {
	for _, symbolic := range []bool{true, false} {
		var raw bytes.Buffer
		w := zip.NewWriter(&raw)
		h := &zip.FileHeader{Name: "index.html", Method: zip.Deflate}
		if symbolic {
			h.SetMode(os.ModeSymlink | 0777)
		} else {
			h.SetMode(0644)
		}
		entry, _ := w.CreateHeader(h)
		if symbolic {
			entry.Write([]byte("/etc/passwd"))
		} else {
			entry.Write(bytes.Repeat([]byte("x"), MaxFileBytes+1))
		}
		w.Close()
		if _, err := ReadArchive(raw.Bytes()); err == nil {
			t.Fatal("unsafe ZIP accepted")
		}
	}
	files := make([]File, MaxFiles+1)
	if _, err := Build(files); err == nil {
		t.Fatal("file count limit ignored")
	}
	if _, err := ReadArchive(make([]byte, MaxArchiveBytes+1)); err == nil {
		t.Fatal("archive size limit ignored")
	}
}

func TestRejectTooManyImplicitDirectories(t *testing.T) {
	files := make([]File, MaxFiles)
	for i := range files {
		files[i] = File{Path: fmt.Sprintf("a%03d/b/c/file.txt", i), Data: []byte("x")}
	}
	if _, err := Build(files); err == nil {
		t.Fatal("implicit directory limit ignored")
	}
}
