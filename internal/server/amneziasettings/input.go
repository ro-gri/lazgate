package amneziasettings

import (
	"encoding/json"
	"laz/internal/server/model"
	"net/netip"
)

// Input is shared by installation and editing; it never accepts server private keys.
type Input struct {
	Version        int64             `json:"config_version_ms"`
	EndpointHost   string            `json:"endpoint_host"`
	EndpointPort   int               `json:"endpoint_port"`
	DNS            string            `json:"dns"`
	ServerIPv4     string            `json:"server_ipv4"`
	Prefix         int               `json:"prefix"`
	MTU            int               `json:"mtu"`
	Keepalive      int               `json:"persistent_keepalive"`
	JC             int               `json:"jc"`
	JMin           int               `json:"jmin"`
	JMax           int               `json:"jmax"`
	S1             int               `json:"s1"`
	S2             int               `json:"s2"`
	S3             int               `json:"s3"`
	S4             int               `json:"s4"`
	H1             string            `json:"h1"`
	H2             string            `json:"h2"`
	H3             string            `json:"h3"`
	H4             string            `json:"h4"`
	I1             string            `json:"i1"`
	I2             string            `json:"i2"`
	I3             string            `json:"i3"`
	I4             string            `json:"i4"`
	I5             string            `json:"i5"`
	HeaderKey      *string           `json:"header_protection_key"`
	Padding        string            `json:"content_padding_addition"`
	Timings        map[string]string `json:"timings"`
	RandomTrailers bool              `json:"random_trailers"`
	DisableCookies bool              `json:"disable_cookies"`
	RouteMode      string            `json:"route_mode"`
	RouteNetworks  string            `json:"route_networks"`
	IPv6Mode       string            `json:"ipv6_mode"`
	IPv6Networks   string            `json:"ipv6_networks"`
}

func (input Input) Apply(c model.AmneziaWGNodeConfig) (model.AmneziaWGNodeConfig, error) {
	invalid := func(message string) (model.AmneziaWGNodeConfig, error) { return c, ValidationError{message} }
	address, err := netip.ParseAddr(input.ServerIPv4)
	if err != nil || !address.Is4() || input.Prefix < 16 || input.Prefix > 30 {
		return invalid("Некорректный IPv4-адрес или префикс подсети")
	}
	routes, err := Routes(input.RouteMode, input.RouteNetworks, input.IPv6Mode, input.IPv6Networks)
	if err != nil {
		return invalid(err.Error())
	}
	c.EndpointHost, c.EndpointPort, c.DNS = input.EndpointHost, input.EndpointPort, input.DNS
	c.ServerIPv4, c.ClientIPv4CIDR = input.ServerIPv4, netip.PrefixFrom(address, input.Prefix).Masked().String()
	c.MTU, c.PersistentKeepalive = input.MTU, input.Keepalive
	c.JC, c.JMin, c.JMax = input.JC, input.JMin, input.JMax
	c.S1, c.S2, c.S3, c.S4 = input.S1, input.S2, input.S3, input.S4
	c.H1, c.H2, c.H3, c.H4 = input.H1, input.H2, input.H3, input.H4
	c.I1, c.I2, c.I3, c.I4, c.I5 = input.I1, input.I2, input.I3, input.I4, input.I5
	if input.HeaderKey != nil {
		if *input.HeaderKey == "" {
			return invalid("Новый ключ не может быть пустым")
		}
		c.HeaderProtectionKey = *input.HeaderKey
	}
	c.ContentPaddingAddition = input.Padding
	timings, _ := json.Marshal(input.Timings)
	c.TimingsJSON = string(timings)
	c.RandomTrailers, c.DisableCookies = input.RandomTrailers, input.DisableCookies
	c.ClientRouteAllowedIPs, c.ClientRouteMode, c.ClientExcludedRoutes = routes, input.RouteMode, ""
	if input.RouteMode == "exclude" {
		c.ClientExcludedRoutes = input.RouteNetworks
	}
	return Prepare(c)
}
