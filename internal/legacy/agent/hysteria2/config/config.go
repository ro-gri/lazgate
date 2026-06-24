package config

import (
	"errors"
	"net"
	"net/url"
	"strings"

	agentcommon "laz/internal/agent/common"
)

type Config struct {
	agentcommon.Config `yaml:",inline" json:",inline"`
	Hysteria2          Hysteria2 `yaml:"hysteria2" json:"hysteria2"`
}

type Hysteria2 struct {
	AuthListen  string `yaml:"auth_listen" json:"auth_listen"`
	StatsURL    string `yaml:"stats_url" json:"stats_url"`
	StatsSecret string `yaml:"stats_secret" json:"stats_secret"`
	ServiceName string `yaml:"service_name" json:"service_name"`
	ConfigPath  string `yaml:"config_path" json:"config_path"`
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
	if strings.TrimSpace(cfg.Hysteria2.AuthListen) == "" {
		cfg.Hysteria2.AuthListen = "127.0.0.1:28262"
	}
	if strings.TrimSpace(cfg.Hysteria2.StatsURL) == "" {
		cfg.Hysteria2.StatsURL = "http://127.0.0.1:25413"
	}
	if strings.TrimSpace(cfg.Hysteria2.ServiceName) == "" {
		cfg.Hysteria2.ServiceName = "hysteria-server"
	}
	if strings.TrimSpace(cfg.Hysteria2.ConfigPath) == "" {
		cfg.Hysteria2.ConfigPath = "/etc/hysteria/config.yaml"
	}
	if cfg.Sync.AuthSyncIntervalSeconds == 0 {
		cfg.Sync.AuthSyncIntervalSeconds = 30
	}
	return cfg
}

func Validate(c Config) error {
	if err := agentcommon.ValidateCommon(c.Config); err != nil {
		return err
	}
	return validateHysteriaConfig(c)
}

func validateHysteriaConfig(c Config) error {
	host, _, err := net.SplitHostPort(c.Hysteria2.AuthListen)
	if err != nil {
		return errors.New("hysteria2.auth_listen must be host:port")
	}
	if host != "127.0.0.1" && host != "localhost" {
		return errors.New("hysteria2.auth_listen must stay on 127.0.0.1")
	}
	statsURL, err := url.Parse(c.Hysteria2.StatsURL)
	if err != nil || statsURL.Scheme == "" || statsURL.Host == "" {
		return errors.New("hysteria2.stats_url must be an absolute URL")
	}
	statsHost, _, err := net.SplitHostPort(statsURL.Host)
	if err != nil {
		statsHost = statsURL.Hostname()
	}
	if statsHost != "127.0.0.1" && statsHost != "localhost" {
		return errors.New("hysteria2.stats_url must stay on localhost")
	}
	return nil
}
