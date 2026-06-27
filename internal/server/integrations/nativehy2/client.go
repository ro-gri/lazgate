package nativehy2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"laz/internal/server/connections/remote"
	"laz/internal/server/model"
)

type Metadata struct {
	PublicDomain string `json:"public_domain"`
	ListenPort   int    `json:"listen_port"`
	ServiceName  string `json:"service_name"`
	ObfsEnabled  bool   `json:"obfs_enabled"`
	ObfsType     string `json:"obfs_type"`
	ObfsPassword string `json:"obfs_password"`
	AgentEnabled bool   `json:"agent_enabled"`
	StatsURL     string `json:"stats_url"`
	NodeCertPEM  string `json:"node_cert_pem,omitempty"`
}

type client struct {
	node  model.Node
	meta  Metadata
	agent remote.AgentControl
}

func New(node model.Node, agent remote.AgentControl) remote.Provider {
	return &client{node: node, meta: ParseMetadata(node.APIKey), agent: agent}
}

func ParseMetadata(raw string) Metadata {
	var meta Metadata
	_ = json.Unmarshal([]byte(raw), &meta)
	if meta.ListenPort == 0 {
		meta.ListenPort = 443
	}
	if meta.ServiceName == "" {
		meta.ServiceName = "hysteria-server"
	}
	if meta.ObfsType == "" {
		meta.ObfsType = "salamander"
	}
	return meta
}

func (c *client) CreateConnection(ctx context.Context, input remote.CreateInput) (remote.Connection, error) {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Password) == "" || strings.TrimSpace(input.CredentialID) == "" {
		return remote.Connection{}, fmt.Errorf("native hy2 requires name, password and credential id")
	}
	return remote.Connection{
		Ref: remote.Ref{ID: input.CredentialID, Name: input.Name},
		Configs: []remote.Config{{
			Kind:        model.ConfigHy2URI,
			Slug:        "hy2",
			Name:        defaultConfigName(input),
			Client:      "happ",
			ContentType: "text/plain; charset=utf-8",
			Value:       c.uri(input.Name, input.Password, defaultConfigName(input)),
		}},
	}, nil
}

func (c *client) ApplyConnection(ctx context.Context, input remote.ApplyInput) error {
	if c.agent == nil || !c.meta.AgentEnabled {
		return nil
	}
	if err := c.agent.RefreshUserAuth(ctx, input.NodeID, input.AccountID, 0); err != nil {
		return err
	}
	return nil
}

func (c *client) SetConnectionStatus(ctx context.Context, ref remote.Ref, status model.Status) error {
	return nil
}

func (c *client) DeleteConnection(ctx context.Context, ref remote.Ref) error {
	return nil
}

func (c *client) ListConnections(ctx context.Context) (remote.ConnectionList, error) {
	return remote.ConnectionList{}, nil
}

func (c *client) uri(name, password, title string) string {
	host := c.meta.PublicDomain
	if host == "" {
		host = c.node.SSHHost
	}
	port := c.meta.ListenPort
	if port == 0 {
		port = 443
	}
	values := url.Values{}
	values.Set("sni", host)
	values.Set("insecure", "0")
	if c.meta.ObfsEnabled {
		values.Set("obfs", c.meta.ObfsType)
		values.Set("obfs-password", c.meta.ObfsPassword)
	}
	return "hy2://" + url.QueryEscape(name+":"+password) + "@" + host + ":" + strconv.Itoa(port) + "/?" + values.Encode() + "#" + url.QueryEscape(title)
}

func (c *client) serviceName() string {
	if c.meta.ServiceName != "" {
		return c.meta.ServiceName
	}
	return "hysteria-server"
}

func defaultConfigName(input remote.CreateInput) string {
	if input.ConfigName != "" {
		return input.ConfigName
	}
	return input.Name + " Hysteria2"
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
