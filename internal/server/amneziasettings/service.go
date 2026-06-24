package amneziasettings

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"go4.org/netipx"
	"laz/internal/server/integrations/nativeawg"
	"laz/internal/server/model"
)

type Store interface {
	GetAmneziaWGNodeConfig(string) (model.AmneziaWGNodeConfig, error)
	ListAmneziaWGConnectionMaterials(string) []model.AmneziaWGConnectionMaterial
	UpdateAmneziaWGNodeConfig(model.AmneziaWGNodeConfig, int64, []model.IssuedConfig) error
}

type ValidationError struct{ Message string }

func (e ValidationError) Error() string { return e.Message }

func Prepare(config model.AmneziaWGNodeConfig) (model.AmneziaWGNodeConfig, error) {
	invalid := func(message string) (model.AmneziaWGNodeConfig, error) { return config, ValidationError{message} }
	server, err := netip.ParseAddr(config.ServerIPv4)
	if err != nil || !server.Is4() {
		return invalid("Адрес сервера должен быть IPv4")
	}
	network, err := netip.ParsePrefix(config.ClientIPv4CIDR)
	if err != nil || !network.Addr().Is4() || network.Bits() < 16 || network.Bits() > 30 || !network.Contains(server) {
		return invalid("Поддерживается IPv4-подсеть /16–/30, содержащая адрес сервера")
	}
	network = network.Masked()
	addressRange := netipx.RangeOfPrefix(network)
	if server == addressRange.From() || server == addressRange.To() {
		return invalid("Адрес сервера не может быть адресом сети или broadcast")
	}
	config.FirstClientIPv4 = addressRange.From().Next().String()
	config.LastClientIPv4 = addressRange.To().Prev().String()
	config.ClientIPv4CIDR = network.String()
	config.ListenPort = config.EndpointPort
	if config.MTU < 68 {
		return invalid("MTU должен быть не меньше 68")
	}
	if err := nativeawg.ValidateNodeConfig(config); err != nil {
		return invalid(err.Error())
	}
	return config, nil
}

func Save(st Store, config model.AmneziaWGNodeConfig, expected int64) (model.AmneziaWGNodeConfig, error) {
	config, err := Prepare(config)
	if err != nil {
		return config, err
	}
	invalid := func(message string) (model.AmneziaWGNodeConfig, error) { return config, ValidationError{message} }
	server, _ := netip.ParseAddr(config.ServerIPv4)
	network, _ := netip.ParsePrefix(config.ClientIPv4CIDR)
	addressRange := netipx.RangeOfPrefix(network)
	var issued []model.IssuedConfig
	for _, material := range st.ListAmneziaWGConnectionMaterials(config.NodeID) {
		address, err := netip.ParseAddr(material.AssignedIPv4)
		if err != nil || !network.Contains(address) || address == server || address == addressRange.From() || address == addressRange.To() {
			return invalid("Новая подсеть или адрес сервера конфликтует с выданными адресами клиентов")
		}
		conf, err := nativeawg.RenderClientConf(config, material)
		if err != nil {
			return invalid(err.Error())
		}
		issued = append(issued, model.IssuedConfig{ConnectionID: material.ConnectionID, Config: conf})
	}
	config.ConfigVersionMS = max(time.Now().UnixMilli(), expected+1)
	config.ConfigErrorID = ""
	config.UpdatedAt = time.Now().UTC()
	if err := st.UpdateAmneziaWGNodeConfig(config, expected, issued); err != nil {
		return config, err
	}
	return config, nil
}

func Routes(mode, ipv4, ipv6Mode, ipv6 string) (string, error) {
	var builder netipx.IPSetBuilder
	parse := func(raw string, version bool) ([]netip.Prefix, error) {
		var result []netip.Prefix
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' }) {
			prefix, err := netip.ParsePrefix(part)
			if err != nil || prefix.Addr().Is4() != version {
				return nil, fmt.Errorf("Некорректная сеть: %s", part)
			}
			result = append(result, prefix.Masked())
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("Укажите хотя бы одну сеть")
		}
		return result, nil
	}
	switch mode {
	case "all":
		builder.AddPrefix(netip.MustParsePrefix("0.0.0.0/0"))
	case "include", "exclude":
		prefixes, err := parse(ipv4, true)
		if err != nil {
			return "", err
		}
		if mode == "exclude" {
			builder.AddPrefix(netip.MustParsePrefix("0.0.0.0/0"))
		}
		for _, p := range prefixes {
			if mode == "exclude" {
				builder.RemovePrefix(p)
			} else {
				builder.AddPrefix(p)
			}
		}
	default:
		return "", fmt.Errorf("Некорректный режим IPv4")
	}
	switch ipv6Mode {
	case "off":
	case "all":
		builder.AddPrefix(netip.MustParsePrefix("::/0"))
	case "include":
		prefixes, err := parse(ipv6, false)
		if err != nil {
			return "", err
		}
		for _, p := range prefixes {
			builder.AddPrefix(p)
		}
	default:
		return "", fmt.Errorf("Некорректный режим IPv6")
	}
	set, err := builder.IPSet()
	if err != nil {
		return "", err
	}
	var values []string
	for _, p := range set.Prefixes() {
		values = append(values, p.String())
	}
	if len(values) == 0 {
		return "", fmt.Errorf("Маршруты не могут быть пустыми")
	}
	return strings.Join(values, ", "), nil
}
