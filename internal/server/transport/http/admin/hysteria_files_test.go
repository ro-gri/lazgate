package admin

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"laz/internal/protocol/hy2config"
	"laz/internal/protocol/hy2files"
	"laz/internal/server/hysteriasettings"
	"laz/internal/server/model"
	store "laz/internal/server/storage"
)

func TestHysteriaFileUploadZIPDeleteAndSecretFreeMetadata(t *testing.T) {
	raw, err := store.OpenSQLite(t.TempDir() + "/db")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	st, err := store.WrapSecrets(raw, "file-key")
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: st}
	upload := func(field, name string, data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, err := w.CreateFormFile(field, name)
		if err != nil {
			t.Fatal(err)
		}
		part.Write(data)
		w.Close()
		r := httptest.NewRequest("POST", "/", &body)
		r.Header.Set("Content-Type", w.FormDataContentType())
		response := httptest.NewRecorder()
		app.hysteriaFiles(response, r)
		return response
	}
	bundle, err := hy2files.Build([]hy2files.File{{Path: "index.html", Data: []byte("private-test-content")}, {Path: "css/site.css", Data: []byte("body{}")}})
	if err != nil {
		t.Fatal(err)
	}
	response := upload("archive", "site.zip", bundle.Archive)
	if response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private-test-content") || strings.Contains(response.Body.String(), "archive") {
		t.Fatal("upload returned file content")
	}
	var manifest hy2files.Manifest
	if err = json.Unmarshal(response.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 || manifest.ID != bundle.Manifest.ID {
		t.Fatal("bad manifest")
	}
	stored, err := raw.GetHysteriaFileSet(manifest.ID)
	if err != nil || !strings.HasPrefix(stored.Archive, "enc:v1:") {
		t.Fatal("archive not encrypted")
	}
	body, _ := json.Marshal(map[string]any{"source_id": manifest.ID, "remove_paths": []string{"css/site.css"}})
	r := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	app.hysteriaFiles(response, r)
	if response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var next hy2files.Manifest
	json.Unmarshal(response.Body.Bytes(), &next)
	if len(next.Files) != 1 || next.ID == manifest.ID {
		t.Fatal("file removal did not create a new set")
	}
	original, _ := st.GetHysteriaFileSetManifest(manifest.ID)
	if !strings.Contains(original, "css/site.css") {
		t.Fatal("source set was modified")
	}
	if response := upload("files", "../index.html", []byte("bad")); response.Code != 400 {
		t.Fatal("unsafe filename accepted")
	}
	if response := upload("files", "index.html", []byte("individual")); response.Code != 200 {
		t.Fatalf("individual upload: %s", response.Body.String())
	}
	if response := upload("archive", "bad.zip", []byte("not zip")); response.Code != 400 {
		t.Fatal("bad ZIP accepted")
	}
	draft := upload("files", "about.html", []byte("draft without index"))
	if draft.Code != 200 {
		t.Fatal("draft upload without index rejected")
	}
	var draftManifest hy2files.Manifest
	if err := json.Unmarshal(draft.Body.Bytes(), &draftManifest); err != nil {
		t.Fatal(err)
	}
	node, err := st.CreateNode(model.Node{Name: "H2", Type: model.NodeTypeNativeHy2})
	if err != nil {
		t.Fatal(err)
	}
	settings := hy2config.Defaults()
	snapshot, _ := json.Marshal(settings)
	config, err := st.CreateHysteria2NodeConfig(model.Hysteria2NodeConfig{NodeID: node.ID, EndpointHost: "h2.example.com", SettingsJSON: string(snapshot)})
	if err != nil {
		t.Fatal(err)
	}
	settings.Masquerade.Type = "file"
	badSave, _ := json.Marshal(hysteriasettings.Input{Version: config.ConfigVersionMS, EndpointHost: config.EndpointHost, EndpointPort: config.EndpointPort, FileSetID: draftManifest.ID, Settings: settings})
	saveResponse := httptest.NewRecorder()
	app.hysteriaSettings(saveResponse, httptest.NewRequest("PUT", "/", bytes.NewReader(badSave)), node.ID)
	if saveResponse.Code != 400 || !strings.Contains(saveResponse.Body.String(), "index.html") {
		t.Fatalf("missing entrypoint accepted: %d %s", saveResponse.Code, saveResponse.Body.String())
	}
	unchanged, _ := st.GetHysteria2NodeConfig(node.ID)
	if unchanged.ConfigVersionMS != config.ConfigVersionMS {
		t.Fatal("invalid save changed the config")
	}
	body, _ = json.Marshal(map[string]any{"source_id": manifest.ID, "remove_paths": []string{"index.html"}})
	r = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	app.hysteriaFiles(response, r)
	if response.Code != 200 {
		t.Fatal("deleting index from draft rejected", response.Body.String())
	}
}
