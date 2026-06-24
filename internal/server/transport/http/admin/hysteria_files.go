package admin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"laz/internal/protocol/hy2files"
	auditsvc "laz/internal/server/audit"
	"laz/internal/server/model"
	"laz/internal/server/transport/http/httpx"
)

func (a *App) hysteriaFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, hy2files.MaxUploadBytes)
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var files []hy2files.File
	var err error
	switch contentType {
	case "multipart/form-data":
		files, err = readHysteriaUpload(r)
	case "application/json":
		var input struct {
			SourceID string   `json:"source_id"`
			Remove   []string `json:"remove_paths"`
		}
		d := json.NewDecoder(io.LimitReader(r.Body, 32768))
		d.DisallowUnknownFields()
		if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF || !hy2files.ValidID(input.SourceID) || len(input.Remove) == 0 || len(input.Remove) > hy2files.MaxFiles {
			httpx.Error(w, 400, "Некорректный запрос удаления файлов")
			return
		}
		source, e := a.store.GetHysteriaFileSet(input.SourceID)
		if e != nil {
			httpx.StoreError(w, e)
			return
		}
		raw, e := base64.StdEncoding.DecodeString(source.Archive)
		if e != nil {
			httpx.StoreError(w, e)
			return
		}
		files, err = hy2files.Verify(input.SourceID, raw)
		remove := map[string]bool{}
		for _, name := range input.Remove {
			if !hy2files.ValidPath(name) {
				httpx.Error(w, 400, "Некорректный путь файла")
				return
			}
			remove[name] = true
		}
		kept := files[:0]
		for _, file := range files {
			if !remove[file.Path] {
				kept = append(kept, file)
			} else {
				delete(remove, file.Path)
			}
		}
		files = kept
		if len(remove) > 0 {
			httpx.Error(w, 400, "Файл не найден в наборе")
			return
		}
		if len(files) == 0 && err == nil {
			httpx.JSON(w, 200, hy2files.Manifest{Files: []hy2files.Entry{}})
			return
		}
	default:
		httpx.Error(w, 415, "Ожидаются файлы, ZIP или JSON")
		return
	}
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			httpx.Error(w, 413, "Превышен размер загрузки")
		} else {
			httpx.Error(w, 400, err.Error())
		}
		return
	}
	bundle, err := hy2files.Build(files)
	if err != nil {
		httpx.Error(w, 400, err.Error())
		return
	}
	manifest, err := json.Marshal(bundle.Manifest)
	if err != nil {
		httpx.StoreError(w, err)
		return
	}
	// Only unreferenced drafts expire. Desired and last-applied file sets remain pinned by foreign keys.
	if err = a.store.PruneHysteriaFileSets(time.Now().Add(-24 * time.Hour)); err != nil {
		httpx.StoreError(w, err)
		return
	}
	if err = a.store.PutHysteriaFileSet(model.HysteriaFileSet{ID: bundle.Manifest.ID, Archive: base64.StdEncoding.EncodeToString(bundle.Archive), ManifestJSON: string(manifest)}); err != nil {
		httpx.StoreError(w, err)
		return
	}
	a.recordAudit(r, auditsvc.Event{Action: "nodes.hysteria.files.upload", EntityType: "hysteria_fileset", EntityID: bundle.Manifest.ID, Details: map[string]any{"files": len(bundle.Manifest.Files), "bytes": bundle.Manifest.Size}})
	httpx.JSON(w, 200, bundle.Manifest)
}

func readHysteriaUpload(r *http.Request) ([]hy2files.File, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, errors.New("Некорректная загрузка")
	}
	var files []hy2files.File
	archiveSeen := false
	total := 0
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		_, params, e := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if e != nil {
			return nil, errors.New("Некорректное имя файла")
		}
		name := params["filename"]
		if !hy2files.ValidPath(name) {
			return nil, errors.New("Недопустимое имя файла")
		}
		switch part.FormName() {
		case "archive":
			if archiveSeen || len(files) > 0 {
				return nil, errors.New("Загрузите один ZIP или отдельные файлы")
			}
			archiveSeen = true
			raw, e := io.ReadAll(io.LimitReader(part, hy2files.MaxArchiveBytes+1))
			if e != nil {
				return nil, e
			}
			files, e = hy2files.ReadArchive(raw)
			if e != nil {
				return nil, e
			}
		case "files":
			if archiveSeen || len(files) >= hy2files.MaxFiles {
				return nil, errors.New("Разрешено не больше 128 файлов или один ZIP")
			}
			raw, e := io.ReadAll(io.LimitReader(part, hy2files.MaxFileBytes+1))
			if e != nil {
				return nil, e
			}
			if len(raw) > hy2files.MaxFileBytes {
				return nil, errors.New("Размер одного файла превышает 4 МиБ")
			}
			total += len(raw)
			if total > hy2files.MaxExpandedBytes {
				return nil, errors.New("Размер файлов превышает 8 МиБ")
			}
			files = append(files, hy2files.File{Path: name, Data: raw})
		default:
			return nil, errors.New("Неизвестное поле загрузки")
		}
		if err := part.Close(); err != nil {
			return nil, err
		}
	}
	return files, nil
}
