// Package hy2files validates and builds bounded, content-addressed static file sets.
package hy2files

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ManagedDirectory = "/var/lib/lazgate-hysteria-files"
	MaxFiles         = 128
	MaxDirectories   = 256
	MaxExpandedBytes = 8 << 20
	MaxFileBytes     = 4 << 20
	MaxArchiveBytes  = 2 << 20
	MaxUploadBytes   = MaxExpandedBytes + (1 << 20)
)

type File struct {
	Path string
	Data []byte
}
type Entry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}
type Manifest struct {
	ID    string  `json:"id"`
	Files []Entry `json:"files"`
	Size  int64   `json:"size"`
}
type Bundle struct {
	Manifest Manifest
	Archive  []byte
}

func ValidID(id string) bool {
	raw, err := hex.DecodeString(id)
	return err == nil && len(raw) == 32 && id == strings.ToLower(id)
}
func ValidPath(name string) bool {
	return len(name) <= 240 && utf8.ValidString(name) && fs.ValidPath(name) && name != "." && !strings.ContainsAny(name, "\\:") && !strings.ContainsFunc(name, unicode.IsControl)
}
func ReadArchive(raw []byte) ([]File, error) {
	if len(raw) > MaxArchiveBytes {
		return nil, errors.New("ZIP превышает 2 МиБ")
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, errors.New("Некорректный ZIP-архив")
	}
	if len(z.File) > MaxFiles+MaxDirectories {
		return nil, errors.New("В архиве слишком много записей")
	}
	var files []File
	total := int64(0)
	names := map[string]bool{}
	for _, entry := range z.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if !ValidPath(name) || names[name] {
			return nil, fmt.Errorf("Недопустимый или повторяющийся путь: %s", name)
		}
		names[name] = true
		mode := entry.Mode()
		if mode.IsDir() {
			continue
		}
		if !mode.IsRegular() {
			return nil, fmt.Errorf("Разрешены только обычные файлы: %s", name)
		}
		if len(files) >= MaxFiles || entry.UncompressedSize64 > MaxFileBytes || entry.UncompressedSize64 > uint64(MaxExpandedBytes-total) {
			return nil, errors.New("Превышен лимит количества или размера файлов")
		}
		r, e := entry.Open()
		if e != nil {
			return nil, errors.New("Не удалось прочитать ZIP")
		}
		data, e := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
		closeErr := r.Close()
		if e != nil || closeErr != nil || len(data) > MaxFileBytes {
			return nil, errors.New("Повреждённый или слишком большой файл ZIP")
		}
		total += int64(len(data))
		if total > MaxExpandedBytes {
			return nil, errors.New("Размер файлов превышает 8 МиБ")
		}
		files = append(files, File{Path: name, Data: data})
	}
	return files, nil
}
func Build(files []File) (Bundle, error) {
	if len(files) == 0 || len(files) > MaxFiles {
		return Bundle{}, errors.New("Нужно от 1 до 128 файлов")
	}
	files = slices.Clone(files)
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	defer writer.Close()
	manifest := Manifest{Files: []Entry{}}
	names := map[string]bool{}
	directories := map[string]bool{}
	for _, file := range files {
		if !ValidPath(file.Path) || names[file.Path] {
			return Bundle{}, fmt.Errorf("Недопустимый или повторяющийся путь: %s", file.Path)
		}
		names[file.Path] = true
		for parent := path.Dir(file.Path); parent != "."; parent = path.Dir(parent) {
			directories[parent] = true
			if len(directories) > MaxDirectories {
				return Bundle{}, errors.New("В наборе больше 256 каталогов")
			}
			if names[parent] {
				return Bundle{}, errors.New("Файл конфликтует с каталогом")
			}
		}
		if len(file.Data) > MaxFileBytes {
			return Bundle{}, errors.New("Размер одного файла превышает 4 МиБ")
		}
		manifest.Size += int64(len(file.Data))
		if manifest.Size > MaxExpandedBytes {
			return Bundle{}, errors.New("Размер файлов превышает 8 МиБ")
		}
		h := &zip.FileHeader{Name: file.Path, Method: zip.Deflate}
		h.SetMode(0644)
		w, err := writer.CreateHeader(h)
		if err != nil {
			return Bundle{}, err
		}
		if _, err = w.Write(file.Data); err != nil {
			return Bundle{}, err
		}
		if output.Len() > MaxArchiveBytes {
			return Bundle{}, errors.New("Сжатый набор превышает 2 МиБ")
		}
		manifest.Files = append(manifest.Files, Entry{Path: file.Path, Size: int64(len(file.Data))})
	}
	if err := writer.Close(); err != nil {
		return Bundle{}, err
	}
	if output.Len() > MaxArchiveBytes {
		return Bundle{}, errors.New("Сжатый набор превышает 2 МиБ")
	}
	sum := sha256.Sum256(output.Bytes())
	manifest.ID = hex.EncodeToString(sum[:])
	return Bundle{Manifest: manifest, Archive: output.Bytes()}, nil
}
func Verify(id string, raw []byte) ([]File, error) {
	if len(raw) > MaxArchiveBytes {
		return nil, errors.New("ZIP превышает 2 МиБ")
	}
	if !ValidID(id) {
		return nil, errors.New("Некорректный ID набора")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != id {
		return nil, errors.New("Контрольная сумма набора не совпадает")
	}
	files, err := ReadArchive(raw)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("Пустой набор файлов")
	}
	_, err = Build(files)
	return files, err
}
