package config

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"

	agentcommon "laz/internal/agent/common"
)

var runtimeObjectName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

const (
	DefaultInterfaceName = "awg0"
)

type Config struct {
	agentcommon.Config `yaml:",inline" json:",inline"`
	AmneziaWG          Runtime `yaml:"amneziawg" json:"amneziawg"`
}

type Runtime struct {
	InterfaceName string `yaml:"interface_name" json:"interface_name"`
	ServiceName   string `yaml:"service_name" json:"service_name"`
	AWGBinaryPath string `yaml:"awg_binary_path" json:"awg_binary_path"`
	UseSudo       bool   `yaml:"use_sudo" json:"use_sudo"`
	ConfigPath    string `yaml:"config_path" json:"config_path"`
}

func LoadFile(path string) (Config, error) {
	var cfg Config
	if err := agentcommon.LoadYAMLFile(path, &cfg); err != nil {
		return Config{}, err
	}
	cfg = WithDefaults(cfg)
	return cfg, Validate(cfg)
}

func WithDefaults(cfg Config) Config {
	cfg.Config = agentcommon.WithCommonDefaults(cfg.Config)
	cfg.AmneziaWG.InterfaceName = defaultString(cfg.AmneziaWG.InterfaceName, DefaultInterfaceName)
	cfg.AmneziaWG.ServiceName = defaultString(cfg.AmneziaWG.ServiceName, "awg-quick@"+cfg.AmneziaWG.InterfaceName)
	cfg.AmneziaWG.AWGBinaryPath = defaultString(cfg.AmneziaWG.AWGBinaryPath, "awg")
	cfg.AmneziaWG.ConfigPath = defaultString(cfg.AmneziaWG.ConfigPath, filepath.Join("/var/lib/lazgate-agent/awg", cfg.AmneziaWG.InterfaceName+".conf"))
	return cfg
}

func Validate(c Config) error {
	if err := agentcommon.ValidateCommon(c.Config); err != nil {
		return err
	}
	if !filepath.IsAbs(c.AmneziaWG.ConfigPath) || strings.ContainsAny(c.AmneziaWG.ConfigPath, "\r\n\x00") {
		return errors.New("amneziawg.config_path must be an absolute path")
	}
	for name, value := range map[string]string{"interface_name": c.AmneziaWG.InterfaceName} {
		if !runtimeObjectName.MatchString(value) || len(value) > 15 {
			return errors.New("amneziawg." + name + " contains unsupported characters")
		}
	}
	service := strings.TrimSuffix(c.AmneziaWG.ServiceName, ".service")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]*$`).MatchString(service) {
		return errors.New("amneziawg.service_name contains unsupported characters")
	}
	if value := c.AmneziaWG.AWGBinaryPath; value == "" || strings.HasPrefix(value, "-") || strings.ContainsAny(value, " \t\r\n\x00") {
		return errors.New("amneziawg.awg_binary_path must be one executable path")
	}
	return nil
}

func defaultString(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
