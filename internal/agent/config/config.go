package config

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	NodeID          string    `yaml:"node_id" json:"node_id"`
	ServerURL       string    `yaml:"server_url" json:"server_url"`
	AgentGRPCTarget string    `yaml:"agent_grpc_target" json:"agent_grpc_target"`
	StatePath       string    `yaml:"state_path" json:"state_path"`
	TransportPath   string    `yaml:"transport_path" json:"transport_path"`
	MTLS            MTLS      `yaml:"mtls" json:"mtls"`
	Hysteria2       Hysteria2 `yaml:"hysteria2" json:"hysteria2"`
	Sync            Sync      `yaml:"sync" json:"sync"`
	Quota           Quota     `yaml:"quota" json:"quota"`
	Runtime         Runtime   `yaml:"runtime" json:"runtime"`
}

type MTLS struct {
	CAFile   string `yaml:"ca_file" json:"ca_file"`
	CertFile string `yaml:"cert_file" json:"cert_file"`
	KeyFile  string `yaml:"key_file" json:"key_file"`
}

type Hysteria2 struct {
	AuthListen  string `yaml:"auth_listen" json:"auth_listen"`
	StatsURL    string `yaml:"stats_url" json:"stats_url"`
	StatsSecret string `yaml:"stats_secret" json:"stats_secret"`
	ServiceName string `yaml:"service_name" json:"service_name"`
	ConfigPath  string `yaml:"config_path" json:"config_path"`
}

type Sync struct {
	AuthSyncIntervalSeconds       int   `yaml:"auth_sync_interval_seconds" json:"auth_sync_interval_seconds"`
	TrafficCollectIntervalSeconds int   `yaml:"traffic_collect_interval_seconds" json:"traffic_collect_interval_seconds"`
	OnlineCollectIntervalSeconds  int   `yaml:"online_collect_interval_seconds" json:"online_collect_interval_seconds"`
	HeartbeatIntervalSeconds      int   `yaml:"heartbeat_interval_seconds" json:"heartbeat_interval_seconds"`
	ReconnectMinBackoffSeconds    int   `yaml:"reconnect_min_backoff_seconds" json:"reconnect_min_backoff_seconds"`
	ReconnectMaxBackoffSeconds    int   `yaml:"reconnect_max_backoff_seconds" json:"reconnect_max_backoff_seconds"`
	UsageQueueMaxBytes            int64 `yaml:"usage_queue_max_bytes" json:"usage_queue_max_bytes"`
	UsageQueueMaxAgeDays          int   `yaml:"usage_queue_max_age_days" json:"usage_queue_max_age_days"`
}

type Quota struct {
	DefaultGuardOverageBytes int64 `yaml:"default_guard_overage_bytes" json:"default_guard_overage_bytes"`
}

type Runtime struct {
	LogLines int `yaml:"log_lines" json:"log_lines"`
}

func LoadFile(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	cfg = WithDefaults(cfg)
	return cfg, cfg.Validate()
}

func WithDefaults(cfg Config) Config {
	cfg.NodeID = strings.TrimSpace(cfg.NodeID)
	cfg.ServerURL = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	cfg.AgentGRPCTarget = strings.TrimSpace(cfg.AgentGRPCTarget)
	cfg.StatePath = strings.TrimSpace(cfg.StatePath)
	if cfg.StatePath == "" {
		cfg.StatePath = "/var/lib/lazgate-agent/state.db"
	}
	cfg.TransportPath = strings.TrimSpace(cfg.TransportPath)
	if cfg.TransportPath == "" {
		cfg.TransportPath = filepath.Join(filepath.Dir(cfg.StatePath), "transport.db")
	}
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
	if cfg.Sync.TrafficCollectIntervalSeconds == 0 {
		cfg.Sync.TrafficCollectIntervalSeconds = 60
	}
	if cfg.Sync.OnlineCollectIntervalSeconds == 0 {
		cfg.Sync.OnlineCollectIntervalSeconds = 30
	}
	if cfg.Sync.HeartbeatIntervalSeconds == 0 {
		cfg.Sync.HeartbeatIntervalSeconds = 30
	}
	if cfg.Sync.ReconnectMinBackoffSeconds == 0 {
		cfg.Sync.ReconnectMinBackoffSeconds = 1
	}
	if cfg.Sync.ReconnectMaxBackoffSeconds == 0 {
		cfg.Sync.ReconnectMaxBackoffSeconds = 60
	}
	if cfg.Sync.UsageQueueMaxBytes == 0 {
		cfg.Sync.UsageQueueMaxBytes = 1 << 30
	}
	if cfg.Sync.UsageQueueMaxAgeDays == 0 {
		cfg.Sync.UsageQueueMaxAgeDays = 30
	}
	if cfg.Quota.DefaultGuardOverageBytes == 0 {
		cfg.Quota.DefaultGuardOverageBytes = 100 * 1024 * 1024
	}
	if cfg.Runtime.LogLines == 0 {
		cfg.Runtime.LogLines = 80
	}
	return cfg
}

func (c Config) Validate() error {
	if c.NodeID == "" {
		return errors.New("node_id is required")
	}
	if c.ServerURL == "" {
		return errors.New("server_url is required")
	}
	u, err := url.Parse(c.ServerURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("server_url must be an absolute URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("server_url must use http or https")
	}
	if c.AgentGRPCTarget != "" {
		if _, _, err := net.SplitHostPort(c.AgentGRPCTarget); err != nil {
			return errors.New("agent_grpc_target must be host:port")
		}
	}
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
	if c.MTLS.CertFile == "" || c.MTLS.KeyFile == "" {
		return errors.New("mtls cert_file and key_file are required")
	}
	return nil
}
