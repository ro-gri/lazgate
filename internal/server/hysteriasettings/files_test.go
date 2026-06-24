package hysteriasettings

import (
	"encoding/json"
	"errors"
	"laz/internal/protocol/hy2config"
	"laz/internal/protocol/hy2files"
	"testing"
)

type manifestStore string

func (s manifestStore) GetHysteriaFileSetManifest(string) (string, error) { return string(s), nil }

func TestIndexRequiredOnSaveNotDraftCreation(t *testing.T) {
	for _, name := range []string{"index.html", "site/index.html", "INDEX.HTML", "about.html"} {
		t.Run(name, func(t *testing.T) {
			bundle, err := hy2files.Build([]hy2files.File{{Path: name, Data: []byte("draft")}})
			if err != nil {
				t.Fatal("draft rejected", err)
			}
			raw, _ := json.Marshal(bundle.Manifest)
			st := manifestStore(raw)
			c := hy2config.Defaults()
			c.Masquerade.Type = "file"
			err = PrepareFiles(st, bundle.Manifest.ID, &c)
			if name == "index.html" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, hy2files.ErrMissingIndex) {
				t.Fatalf("missing index accepted: %v", err)
			}
			c.Masquerade.Type = "off"
			if err := PrepareFiles(st, bundle.Manifest.ID, &c); err != nil {
				t.Fatal("inactive draft blocked save", err)
			}
		})
	}
	c := hy2config.Defaults()
	c.Masquerade.Type = "file"
	if err := PrepareFiles(manifestStore("{}"), "", &c); !errors.Is(err, hy2files.ErrMissingIndex) {
		t.Fatal("empty draft accepted")
	}
}
