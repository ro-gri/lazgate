package view

import (
	"encoding/json"
	"laz/internal/server/model"
)

// AmneziaSettings is a secret-free projection for the settings form.
type AmneziaSettings struct {
	ConfigVersionMS        int64             `json:"config_version_ms"`
	AppliedConfigVersionMS int64             `json:"applied_config_version_ms"`
	ConfigErrorID          string            `json:"config_error_id"`
	Timings                map[string]string `json:"timings"`
	RandomTrailers         bool              `json:"random_trailers"`
	DisableCookies         bool              `json:"disable_cookies"`
	ClientRouteMode        string            `json:"client_route_mode"`
	ClientExcludedRoutes   string            `json:"client_excluded_routes"`
	EndpointHost           string            `json:"endpoint_host"`
	EndpointPort           int               `json:"endpoint_port"`
	DNS                    string            `json:"dns"`
	ServerIPv4             string            `json:"server_ipv4"`
	ClientIPv4CIDR         string            `json:"client_ipv4_cidr"`
	MTU                    int               `json:"mtu"`
	PersistentKeepalive    int               `json:"persistent_keepalive"`
	ClientRouteAllowedIPs  string            `json:"client_route_allowed_ips"`
	JC                     int               `json:"jc"`
	JMin                   int               `json:"jmin"`
	JMax                   int               `json:"jmax"`
	S1                     int               `json:"s1"`
	S2                     int               `json:"s2"`
	S3                     int               `json:"s3"`
	S4                     int               `json:"s4"`
	H1                     string            `json:"h1"`
	H2                     string            `json:"h2"`
	H3                     string            `json:"h3"`
	H4                     string            `json:"h4"`
	I1                     string            `json:"i1"`
	I2                     string            `json:"i2"`
	I3                     string            `json:"i3"`
	I4                     string            `json:"i4"`
	I5                     string            `json:"i5"`
	ContentPaddingAddition string            `json:"content_padding_addition"`
	HeaderProtectionKeySet bool              `json:"header_protection_key_set"`
}

func HeaderProtectionConfigured(value string) bool { return value != "" }
func AmneziaTimings(value string) map[string]string {
	result := map[string]string{}
	_ = json.Unmarshal([]byte(value), &result)
	return result
}

func AmneziaSettingsItem(config model.AmneziaWGNodeConfig) AmneziaSettings {
	return converter.ToAmneziaSettings(config)
}
