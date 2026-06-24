package common

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	NodeID          string  `yaml:"node_id" json:"node_id"`
	InstallationID  string  `yaml:"installation_id" json:"installation_id"`
	AgentType       string  `yaml:"agent_type" json:"agent_type"`
	ServerURL       string  `yaml:"server_url" json:"server_url"`
	AgentGRPCTarget string  `yaml:"agent_grpc_target" json:"agent_grpc_target"`
	StatePath       string  `yaml:"state_path" json:"state_path"`
	TransportPath   string  `yaml:"transport_path" json:"transport_path"`
	MTLS            MTLS    `yaml:"mtls" json:"mtls"`
	Sync            Sync    `yaml:"sync" json:"sync"`
	Runtime         Runtime `yaml:"runtime" json:"runtime"`
}

func (c Config) Identity() Identity {
	return Identity{
		NodeID:         c.NodeID,
		InstallationID: c.InstallationID,
		AgentType:      c.AgentType,
	}
}

type MTLS struct {
	CAFile   string `yaml:"ca_file" json:"ca_file"`
	CertFile string `yaml:"cert_file" json:"cert_file"`
	KeyFile  string `yaml:"key_file" json:"key_file"`
}

type Sync struct {
	AuthSyncIntervalSeconds         int `yaml:"auth_sync_interval_seconds" json:"auth_sync_interval_seconds"`
	TelemetryCollectIntervalSeconds int `yaml:"telemetry_collect_interval_seconds" json:"telemetry_collect_interval_seconds"`
	HeartbeatIntervalSeconds        int `yaml:"heartbeat_interval_seconds" json:"heartbeat_interval_seconds"`
	ReconnectMinBackoffSeconds      int `yaml:"reconnect_min_backoff_seconds" json:"reconnect_min_backoff_seconds"`
	ReconnectMaxBackoffSeconds      int `yaml:"reconnect_max_backoff_seconds" json:"reconnect_max_backoff_seconds"`
}

type Runtime struct {
	LogLines int `yaml:"log_lines" json:"log_lines"`
}

func LoadYAMLFile(path string, cfg any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(raw, cfg)
}

func WithCommonDefaults(cfg Config) Config {
	cfg.NodeID = strings.TrimSpace(cfg.NodeID)
	cfg.InstallationID = strings.TrimSpace(cfg.InstallationID)
	cfg.AgentType = strings.TrimSpace(cfg.AgentType)
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
	if cfg.Sync.AuthSyncIntervalSeconds == 0 {
		cfg.Sync.AuthSyncIntervalSeconds = 30
	}
	if cfg.Sync.TelemetryCollectIntervalSeconds == 0 {
		cfg.Sync.TelemetryCollectIntervalSeconds = 30
	}
	if cfg.Sync.HeartbeatIntervalSeconds == 0 {
		cfg.Sync.HeartbeatIntervalSeconds = 30
	}
	if cfg.Sync.ReconnectMinBackoffSeconds == 0 {
		cfg.Sync.ReconnectMinBackoffSeconds = 1
	}
	if cfg.Sync.ReconnectMaxBackoffSeconds == 0 {
		cfg.Sync.ReconnectMaxBackoffSeconds = 300
	}
	if cfg.Runtime.LogLines == 0 {
		cfg.Runtime.LogLines = 80
	}
	return cfg
}

func ValidateCommon(c Config) error {
	if c.NodeID == "" {
		return errors.New("node_id is required")
	}
	if c.InstallationID == "" || c.AgentType == "" {
		return errors.New("installation_id and agent_type are required")
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
	if c.MTLS.CAFile == "" || c.MTLS.CertFile == "" || c.MTLS.KeyFile == "" {
		return errors.New("mtls ca_file, cert_file, and key_file are required")
	}
	return nil
}

func newGRPCClientConfig(identity Identity, cfg Config, agentVersion, protocolVersion string) GRPCClientConfig {
	target := cfg.AgentGRPCTarget
	if target == "" {
		target = GRPCTargetFromURL(cfg.ServerURL)
	}
	return GRPCClientConfig{
		Target:          target,
		TLSConfig:       func() (*tls.Config, error) { return MTLSConfig(cfg.MTLS) },
		Identity:        identity,
		AgentVersion:    agentVersion,
		ProtocolVersion: protocolVersion,
	}
}

func GRPCTargetFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":9443"
	}
	return host
}

func MTLSConfig(cfg MTLS) (*tls.Config, error) {
	if cfg.CAFile == "" {
		return nil, fmt.Errorf("mtls ca_file is required")
	}
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, err
	}
	caRaw, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caRaw) {
		return nil, fmt.Errorf("failed to parse CA bundle")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, RootCAs: pool}, nil
}
