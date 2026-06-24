package nativeawg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/net/idna"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"laz/internal/protocol/awgconfig"
	"laz/internal/server/model"
)

const (
	DefaultDNS                    = "1.1.1.1, 1.0.0.1"
	DefaultMTU                    = 1376
	DefaultPersistentKeepalive    = 25
	DefaultClientRouteAllowedIPs  = "0.0.0.0/0"
	DefaultDisabledPeerAllowedIPs = "0.0.0.0/32"
	DefaultAssignedPrefix         = 32
	DefaultServerPrefix           = 24
	MaxParameterValue             = 65535
	DefaultServerIPv4             = "10.8.1.1"
	DefaultClientIPv4CIDR         = "10.8.1.0/24"
	DefaultFirstClientIPv4        = "10.8.1.2"
	DefaultLastClientIPv4         = "10.8.1.254"
)

func ValidateNodeConfig(config model.AmneziaWGNodeConfig) error {
	config.ServerIPv4 = defaultString(config.ServerIPv4, DefaultServerIPv4)
	config.ClientIPv4CIDR = defaultString(config.ClientIPv4CIDR, DefaultClientIPv4CIDR)
	config.FirstClientIPv4 = defaultString(config.FirstClientIPv4, DefaultFirstClientIPv4)
	config.LastClientIPv4 = defaultString(config.LastClientIPv4, DefaultLastClientIPv4)
	if _, err := validateEndpointHost(config.EndpointHost); err != nil {
		return err
	}
	if err := validatePort("endpoint port", config.EndpointPort); err != nil {
		return err
	}
	if err := validatePort("listen port", defaultInt(config.ListenPort, config.EndpointPort)); err != nil {
		return err
	}
	if _, err := validateIPv4Address("server IPv4", config.ServerIPv4); err != nil {
		return err
	}
	if err := validateIPv4Pool(config); err != nil {
		return err
	}
	if _, err := validateDNS(defaultString(config.DNS, DefaultDNS)); err != nil {
		return err
	}
	mtu := defaultInt(config.MTU, DefaultMTU)
	if mtu < 1 || mtu > MaxParameterValue {
		return fmt.Errorf("amneziawg MTU must be between 1 and %d", MaxParameterValue)
	}
	keepalive := config.PersistentKeepalive
	if keepalive < 0 || keepalive > MaxParameterValue {
		return fmt.Errorf("amneziawg persistent keepalive must be between 0 and %d", MaxParameterValue)
	}
	if _, err := validateAllowedRoutes(defaultString(config.ClientRouteAllowedIPs, DefaultClientRouteAllowedIPs)); err != nil {
		return err
	}
	if _, err := validateConfigValue("server private key", config.ServerPrivateKey); err != nil {
		return err
	}
	if _, err := validateConfigValue("server public key", config.ServerPublicKey); err != nil {
		return err
	}
	return validateAmneziaWGParameters(config)
}

func RenderClientConf(config model.AmneziaWGNodeConfig, material model.AmneziaWGConnectionMaterial) (string, error) {
	if err := ValidateNodeConfig(config); err != nil {
		return "", err
	}
	clientPrivateKey, err := validateConfigValue("client private key", material.ClientPrivateKey)
	if err != nil {
		return "", err
	}
	psk, err := validateConfigValue("preshared key", material.PresharedKey)
	if err != nil {
		return "", err
	}
	serverPublicKey, err := validateConfigValue("server public key", config.ServerPublicKey)
	if err != nil {
		return "", err
	}
	endpointHost, err := validateEndpointHost(config.EndpointHost)
	if err != nil {
		return "", err
	}
	if err := validatePort("endpoint port", config.EndpointPort); err != nil {
		return "", err
	}
	assignedIPv4, err := validateIPv4Address("assigned IPv4", material.AssignedIPv4)
	if err != nil {
		return "", err
	}
	dns, err := validateDNS(defaultString(config.DNS, DefaultDNS))
	if err != nil {
		return "", err
	}
	mtu := defaultInt(config.MTU, DefaultMTU)
	keepalive := config.PersistentKeepalive
	allowedIPs, err := validateAllowedRoutes(defaultString(config.ClientRouteAllowedIPs, DefaultClientRouteAllowedIPs))
	if err != nil {
		return "", err
	}

	return renderProfile(config, []string{
		"[Interface]",
		"PrivateKey = " + clientPrivateKey,
		"Address = " + assignedIPv4 + "/" + strconv.Itoa(DefaultAssignedPrefix),
		"DNS = " + dns,
		"MTU = " + strconv.Itoa(mtu),
		"Jc = " + strconv.Itoa(config.JC),
		"Jmin = " + strconv.Itoa(config.JMin),
		"Jmax = " + strconv.Itoa(config.JMax),
		"S1 = " + strconv.Itoa(config.S1),
		"S2 = " + strconv.Itoa(config.S2),
		"S3 = " + strconv.Itoa(config.S3),
		"S4 = " + strconv.Itoa(config.S4),
		"H1 = " + config.H1,
		"H2 = " + config.H2,
		"H3 = " + config.H3,
		"H4 = " + config.H4,
		"I1 = " + config.I1,
		"I2 = " + config.I2,
		"I3 = " + config.I3,
		"I4 = " + config.I4,
		"I5 = " + config.I5,
		"",
		"[Peer]",
		"PublicKey = " + serverPublicKey,
		"PresharedKey = " + psk,
		"Endpoint = " + net.JoinHostPort(endpointHost, strconv.Itoa(config.EndpointPort)),
		"AllowedIPs = " + allowedIPs,
		"PersistentKeepalive = " + strconv.Itoa(keepalive),
		"",
	}), nil
}

func RenderServerConfig(config model.AmneziaWGNodeConfig, connections []model.AmneziaWGSyncMaterial) (string, error) {
	if err := ValidateNodeConfig(config); err != nil {
		return "", err
	}
	serverPrivateKey, err := validateConfigValue("server private key", config.ServerPrivateKey)
	if err != nil {
		return "", err
	}
	serverIPv4, err := validateIPv4Address("server IPv4", config.ServerIPv4)
	if err != nil {
		return "", err
	}
	listenPort := defaultInt(config.ListenPort, config.EndpointPort)
	if err := validatePort("listen port", listenPort); err != nil {
		return "", err
	}
	lines := []string{
		"[Interface]",
		"PrivateKey = " + serverPrivateKey,
		"Address = " + serverIPv4 + "/" + strings.Split(config.ClientIPv4CIDR, "/")[1],
		"ListenPort = " + strconv.Itoa(listenPort),
		"Jc = " + strconv.Itoa(config.JC),
		"Jmin = " + strconv.Itoa(config.JMin),
		"Jmax = " + strconv.Itoa(config.JMax),
		"S1 = " + strconv.Itoa(config.S1),
		"S2 = " + strconv.Itoa(config.S2),
		"S3 = " + strconv.Itoa(config.S3),
		"S4 = " + strconv.Itoa(config.S4),
		"H1 = " + config.H1,
		"H2 = " + config.H2,
		"H3 = " + config.H3,
		"H4 = " + config.H4,
		"I1 = " + config.I1,
		"I2 = " + config.I2,
		"I3 = " + config.I3,
		"I4 = " + config.I4,
		"I5 = " + config.I5,
	}
	for _, item := range connections {
		if item.Connection.DesiredStatus == model.ConnectionStatusDeleted {
			continue
		}
		publicKey, err := validateConfigValue("peer public key", item.Material.ClientPublicKey)
		if err != nil {
			return "", err
		}
		psk, err := validateConfigValue("peer preshared key", item.Material.PresharedKey)
		if err != nil {
			return "", err
		}
		allowedIPs, err := ServerConnectionAllowedIPs(item.Connection, item.Material)
		if err != nil {
			return "", err
		}
		lines = append(lines,
			"",
			"[Peer]",
			"PublicKey = "+publicKey,
			"PresharedKey = "+psk,
			"AllowedIPs = "+allowedIPs,
		)
	}
	lines = append(lines, "")
	return renderProfile(config, lines), nil
}

func renderProfile(config model.AmneziaWGNodeConfig, lines []string) string {
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if strings.HasPrefix(line, "I") && strings.HasSuffix(line, " = ") {
			continue
		}
		out = append(out, line)
		if line == "[Interface]" && config.HeaderProtectionKey != "" {
			out = append(out, "HeaderProtectionKey = "+config.HeaderProtectionKey)
			if config.ContentPaddingAddition != "" {
				out = append(out, "ContentPaddingAddition = "+config.ContentPaddingAddition)
			}
		}
		if line == "[Interface]" {
			var timings map[string]string
			_ = json.Unmarshal([]byte(config.TimingsJSON), &timings)
			for _, key := range awgconfig.Timings {
				if timings[key] != "" {
					out = append(out, key+" = "+timings[key])
				}
			}
			out = append(out, "RandomTrailers = "+awgconfig.FormatBool(config.RandomTrailers), "DisableCookies = "+awgconfig.FormatBool(config.DisableCookies))
		}
	}
	return strings.Join(out, "\n")
}

func validateAmneziaWGParameters(config model.AmneziaWGNodeConfig) error {
	var timings map[string]string
	if config.TimingsJSON != "" {
		if err := json.Unmarshal([]byte(config.TimingsJSON), &timings); err != nil {
			return fmt.Errorf("invalid timings")
		}
	}
	if err := awgconfig.Validate([4]string{config.H1, config.H2, config.H3, config.H4}, [5]string{config.I1, config.I2, config.I3, config.I4, config.I5}, config.ContentPaddingAddition, timings); err != nil {
		return err
	}
	if config.HeaderProtectionKey != "" {
		key, err := base64.StdEncoding.DecodeString(config.HeaderProtectionKey)
		if err != nil || len(key) != 32 {
			return fmt.Errorf("amneziawg header protection key must encode 32 bytes")
		}
		if config.S1 < 12 || config.S2 < 12 || config.S3 < 12 || config.S4 < 12 {
			return fmt.Errorf("amneziawg 3.1 S1-S4 must be >= 12")
		}
	}
	values := []struct {
		name  string
		value int
	}{
		{"jc", config.JC}, {"jmin", config.JMin}, {"jmax", config.JMax},
		{"s1", config.S1}, {"s2", config.S2}, {"s3", config.S3}, {"s4", config.S4},
	}
	for _, item := range values {
		if item.value < 0 || item.value > MaxParameterValue {
			return fmt.Errorf("amneziawg %s must be between 1 and %d", item.name, MaxParameterValue)
		}
	}
	if config.JMin > config.JMax {
		return fmt.Errorf("amneziawg jmin must be <= jmax")
	}
	return nil
}

func validateIPv4Pool(config model.AmneziaWGNodeConfig) error {
	_, network, err := net.ParseCIDR(strings.TrimSpace(config.ClientIPv4CIDR))
	if err != nil || network.IP.To4() == nil {
		return fmt.Errorf("amneziawg client IPv4 CIDR must be an IPv4 network")
	}
	server := net.ParseIP(strings.TrimSpace(config.ServerIPv4)).To4()
	first := net.ParseIP(strings.TrimSpace(config.FirstClientIPv4)).To4()
	last := net.ParseIP(strings.TrimSpace(config.LastClientIPv4)).To4()
	if server == nil || first == nil || last == nil {
		return fmt.Errorf("amneziawg IPv4 pool requires valid server, first client and last client addresses")
	}
	if !network.Contains(server) || !network.Contains(first) || !network.Contains(last) {
		return fmt.Errorf("amneziawg server and client IPv4 addresses must belong to client IPv4 CIDR")
	}
	if bytesCompareIPv4(first, last) > 0 {
		return fmt.Errorf("amneziawg first client IPv4 must not exceed last client IPv4")
	}
	return nil
}

func bytesCompareIPv4(a, b net.IP) int {
	for i := 0; i < net.IPv4len; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func NewIssuedAmneziaConf(connectionID, name, conf string) model.IssuedConfig {
	return model.IssuedConfig{
		ConnectionID: connectionID,
		Kind:         model.ConfigAmneziaConf,
		Slug:         "awg-conf",
		Name:         defaultString(name, "AmneziaWG .conf"),
		Client:       "amneziawg",
		ContentType:  "text/plain; charset=utf-8",
		Config:       conf,
	}
}

func ServerConnectionAllowedIPs(connection model.Connection, material model.AmneziaWGConnectionMaterial) (string, error) {
	switch connection.DesiredStatus {
	case "", model.ConnectionStatusActive:
		assignedIPv4, err := validateIPv4Address("active server peer assigned IPv4", material.AssignedIPv4)
		if err != nil {
			return "", err
		}
		return assignedIPv4 + "/" + strconv.Itoa(DefaultAssignedPrefix), nil
	case model.ConnectionStatusHeld:
		return DefaultDisabledPeerAllowedIPs, nil
	case model.ConnectionStatusDeleted:
		return "", fmt.Errorf("amneziawg deleted peer must be omitted")
	default:
		return "", fmt.Errorf("amneziawg server peer has unknown desired status %q", connection.DesiredStatus)
	}
}

func validateConfigValue(label, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("amneziawg config requires %s", label)
	}
	if hasControlChar(trimmed) {
		return "", fmt.Errorf("amneziawg %s contains control characters", label)
	}
	return trimmed, nil
}

func validateEndpointHost(value string) (string, error) {
	host, err := validateConfigValue("endpoint host", value)
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	name, err := idna.Lookup.ToASCII(strings.TrimSuffix(host, "."))
	if err != nil || len(name) > 253 || regexp.MustCompile(`^[0-9.]+$`).MatchString(name) {
		return "", fmt.Errorf("invalid endpoint domain")
	}
	for _, label := range strings.Split(name, ".") {
		if !regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`).MatchString(label) {
			return "", fmt.Errorf("invalid endpoint domain")
		}
	}
	return name, nil
}

func validatePort(label string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("amneziawg %s must be between 1 and 65535", label)
	}
	return nil
}

func validateIPv4Address(label, value string) (string, error) {
	ipText, err := validateConfigValue(label, value)
	if err != nil {
		return "", err
	}
	if strings.Contains(ipText, ":") {
		return "", fmt.Errorf("amneziawg %s must be dotted IPv4", label)
	}
	ip := net.ParseIP(ipText)
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("amneziawg %s must be IPv4", label)
	}
	return ipText, nil
}

func validateDNS(value string) (string, error) {
	entries := splitCSV(value)
	if len(entries) == 0 {
		return "", fmt.Errorf("amneziawg DNS requires at least one IPv4 address")
	}
	for _, entry := range entries {
		if _, err := validateIPv4Address("DNS entry", entry); err != nil {
			return "", err
		}
	}
	return strings.Join(entries, ", "), nil
}

func validateAllowedRoutes(value string) (string, error) {
	routes := splitCSV(value)
	if len(routes) == 0 {
		return "", fmt.Errorf("amneziawg allowed routes require at least one IPv4 CIDR")
	}
	for _, route := range routes {
		ip, cidr, err := net.ParseCIDR(route)
		if err != nil || ip == nil || cidr == nil {
			return "", fmt.Errorf("amneziawg allowed route %q must be a CIDR", route)
		}
	}
	return strings.Join(routes, ", "), nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func hasControlChar(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
