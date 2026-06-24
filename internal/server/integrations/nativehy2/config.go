package nativehy2

import (
	"net/url"
	"strconv"
	"strings"

	"laz/internal/server/model"
)

func URI(config model.Hysteria2NodeConfig, connectionID, password, title string) string {
	values := url.Values{}
	values.Set("sni", config.EndpointHost)
	values.Set("insecure", "0")
	if config.ObfsEnabled {
		values.Set("obfs", config.ObfsType)
		values.Set("obfs-password", config.ObfsPassword)
	}
	return "hy2://" + url.QueryEscape(strings.TrimSpace(connectionID)+":"+password) + "@" + config.EndpointHost + ":" + strconv.Itoa(config.EndpointPort) + "/?" + values.Encode() + "#" + url.QueryEscape(title)
}

func NewIssuedConfig(config model.Hysteria2NodeConfig, connectionID, password, title string) model.IssuedConfig {
	return model.IssuedConfig{
		ConnectionID: connectionID,
		Kind:         model.ConfigHy2URI,
		Slug:         "hy2",
		Name:         title,
		Client:       "happ",
		ContentType:  "text/plain; charset=utf-8",
		Config:       URI(config, connectionID, password, title),
	}
}
