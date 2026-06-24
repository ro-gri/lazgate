package hysteriasettings

import (
	"encoding/json"
	"errors"
	"time"

	"laz/internal/protocol/hy2config"
	"laz/internal/protocol/hy2files"
	"laz/internal/server/connections"
	"laz/internal/server/integrations/nativehy2"
	"laz/internal/server/model"
	"path/filepath"
)

type Input struct {
	FileSetID    string           `json:"file_set_id"`
	Version      int64            `json:"config_version_ms"`
	EndpointHost string           `json:"endpoint_host"`
	EndpointPort int              `json:"endpoint_port"`
	Settings     hy2config.Config `json:"settings"`
}
type View struct {
	Input
	FileSet         *hy2files.Manifest `json:"file_set,omitempty"`
	AppliedVersion  int64              `json:"applied_config_version_ms"`
	ErrorID         string             `json:"config_error_id"`
	ObfsPasswordSet bool               `json:"obfs_password_set"`
}
type Store interface {
	GetHysteriaFileSetManifest(string) (string, error)
	GetNode(string) (model.Node, error)
	GetHysteria2NodeConfig(string) (model.Hysteria2NodeConfig, error)
	UpdateHysteria2NodeConfig(model.Hysteria2NodeConfig, int64, []model.IssuedConfig) error
	ListHysteria2ConfigSources(string) ([]model.Hysteria2ConfigSource, error)
}

type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func Settings(c model.Hysteria2NodeConfig) (hy2config.Config, error) {
	if c.SettingsJSON == "" {
		return hy2config.Config{}, errors.New("Hysteria settings snapshot is missing")
	}
	return hy2config.Decode(c.SettingsJSON)
}
func Public(st Store, c model.Hysteria2NodeConfig) (View, error) {
	s, e := Settings(c)
	if e != nil {
		return View{}, e
	}
	set := s.Obfs.Password != ""
	s.Obfs.Password = ""
	for i := range s.Outbounds {
		s.Outbounds[i].Password = ""
	}
	// URLs may contain proxy credentials; those are preserved when unchanged, not exposed.
	for i := range s.Outbounds {
		if s.Outbounds[i].Type == "http" {
			s.Outbounds[i].URL = redactURL(s.Outbounds[i].URL)
		}
	}
	s.Masquerade.URL = redactURL(s.Masquerade.URL)
	view := View{Input: Input{FileSetID: c.MasqueradeFileSetID, Version: c.ConfigVersionMS, EndpointHost: c.EndpointHost, EndpointPort: c.EndpointPort, Settings: s}, AppliedVersion: c.AppliedConfigVersionMS, ErrorID: c.ConfigErrorID, ObfsPasswordSet: set}
	if c.MasqueradeFileSetID != "" {
		raw, err := st.GetHysteriaFileSetManifest(c.MasqueradeFileSetID)
		if err != nil {
			return view, err
		}
		view.FileSet = &hy2files.Manifest{}
		if err = json.Unmarshal([]byte(raw), view.FileSet); err != nil {
			return view, err
		}
	}
	return view, nil
}

func PrepareFiles(st interface{ GetHysteriaFileSetManifest(string) (string, error) }, id string, c *hy2config.Config) error {
	if id == "" {
		if c.Masquerade.Type == "file" && c.Masquerade.Dir == "" {
			return hy2files.ErrMissingIndex
		}
		return nil
	}
	if !hy2files.ValidID(id) {
		return errors.New("Некорректный ID набора файлов")
	}
	raw, err := st.GetHysteriaFileSetManifest(id)
	if err != nil {
		return errors.New("Набор файлов недоступен. Загрузите его заново")
	}
	if c.Masquerade.Type == "file" {
		var manifest hy2files.Manifest
		if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
			return errors.New("Не удалось прочитать список файлов набора")
		}
		paths := make([]string, 0, len(manifest.Files))
		for _, file := range manifest.Files {
			paths = append(paths, file.Path)
		}
		if err := hy2files.RequireIndex(paths); err != nil {
			return err
		}
	}
	c.Masquerade.Dir = filepath.Join(hy2files.ManagedDirectory, id)
	return nil
}
func Save(st Store, c model.Hysteria2NodeConfig, in Input) (model.Hysteria2NodeConfig, error) {
	if err := PrepareFiles(st, in.FileSetID, &in.Settings); err != nil {
		return c, err
	}
	if !hy2config.Domain(in.EndpointHost) || in.EndpointPort < 1 || in.EndpointPort > 65535 {
		return c, errors.New("Укажите корректный публичный домен и порт 1–65535")
	}
	previous, err := Settings(c)
	if err != nil {
		return c, err
	}
	if in.Settings.Masquerade.Type == "file" && in.FileSetID == "" && (c.MasqueradeFileSetID != "" || previous.Masquerade.Type != "file" || in.Settings.Masquerade.Dir != previous.Masquerade.Dir) {
		return c, ValidationError{"Загрузите файлы или ZIP для маскировки"}
	}
	if in.Settings.Obfs.Password == "" {
		in.Settings.Obfs.Password = previous.Obfs.Password
	}
	for i := range in.Settings.Outbounds {
		for _, old := range previous.Outbounds {
			if old.Name == in.Settings.Outbounds[i].Name && old.Type == in.Settings.Outbounds[i].Type {
				if in.Settings.Outbounds[i].Password == "" {
					in.Settings.Outbounds[i].Password = old.Password
				}
				in.Settings.Outbounds[i].URL = restoreURL(in.Settings.Outbounds[i].URL, old.URL)
			}
		}
	}
	in.Settings.Masquerade.URL = restoreURL(in.Settings.Masquerade.URL, previous.Masquerade.URL)
	// Keep Docker/internal port mapping unless the externally visible port changes.
	if in.EndpointPort != c.EndpointPort {
		in.Settings.ListenPort = in.EndpointPort
	} else {
		in.Settings.ListenPort = previous.ListenPort
	}
	if err := hy2config.Validate(in.Settings); err != nil {
		return c, err
	}
	raw, err := json.Marshal(in.Settings)
	if err != nil {
		return c, err
	}
	c.EndpointHost = in.EndpointHost
	c.EndpointPort = in.EndpointPort
	c.SettingsJSON = string(raw)
	c.MasqueradeFileSetID = in.FileSetID
	c.ObfsEnabled = in.Settings.Obfs.Type != "off"
	c.ObfsType = in.Settings.Obfs.Type
	c.ObfsPassword = in.Settings.Obfs.Password
	c.MasqueradeURL = ""
	if in.Settings.Masquerade.Type == "proxy" {
		c.MasqueradeURL = redactURL(in.Settings.Masquerade.URL)
	}
	c.ConfigVersionMS = max(time.Now().UnixMilli(), in.Version+1)
	c.ConfigErrorID = ""
	c.UpdatedAt = time.Now().UTC()
	var issued []model.IssuedConfig
	node, err := st.GetNode(c.NodeID)
	if err != nil {
		return c, err
	}
	sources, err := st.ListHysteria2ConfigSources(c.NodeID)
	if err != nil {
		return c, err
	}
	for _, source := range sources {
		issued = append(issued, nativehy2.NewIssuedConfig(c, source.ConnectionID, source.Password, connections.ConfigName(node, source.Name)))
	}
	return c, st.UpdateHysteria2NodeConfig(c, in.Version, issued)
}
