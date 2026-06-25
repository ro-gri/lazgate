package subscriptions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"

	"laz/internal/server/model"
	"laz/internal/server/storage"
)

type Encryptor func(ctx context.Context, rawURL string) (string, error)
type ShortIDFunc func() (string, error)
type TargetURLFunc func(id string) string

type Store interface {
	ListConfigProfiles() []model.ConfigProfile
	CreateShortLink(model.ShortLink) (model.ShortLink, error)
	GetShortLinkByTokenProfile(tokenID, profile string) (model.ShortLink, error)
}

type Service struct {
	store Store
}

func New(st Store) *Service {
	return &Service{store: st}
}

type ProfileMeta struct {
	Title    string
	Announce string
	Routing  string
	Found    bool
}

type hpProfileTemplate struct {
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Announce    string            `json:"announce"`
	Routing     *hpRoutingProfile `json:"routing"`
}

type hpRoutingProfile struct {
	Name       string   `json:"name"`
	ProxySites []string `json:"proxy_sites"`
	ProxyIP    []string `json:"proxy_ip"`
}

func (s *Service) HPSubscriptionBody(token model.AccessToken, summary model.AccountSummary) string {
	lines := s.hy2Lines(token, summary)
	return strings.Join(lines, "\n") + "\n"
}

func (s *Service) ClosedSubscriptionBody(token model.AccessToken, summary model.AccountSummary) string {
	body := strings.Join(s.hy2Lines(token, summary), "\n")
	if body != "" {
		body += "\n"
	}
	return base64.StdEncoding.EncodeToString([]byte(body))
}

func (s *Service) ProfileMeta(profile string) ProfileMeta {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile = "all"
	}
	if s.store != nil {
		for _, item := range s.store.ListConfigProfiles() {
			if item.Status != model.StatusActive || item.Slug != profile || item.Client != "happ" || item.Kind != model.ConfigKind("hp_subscription") {
				continue
			}
			parsed := parseHPProfileTemplate(item.ConfigTemplate)
			if parsed.Type != "happ_subscription_profile" {
				continue
			}
			title := strings.TrimSpace(parsed.Title)
			if title == "" {
				title = item.Name
			}
			if strings.TrimSpace(title) == "" {
				title = item.Slug
			}
			return ProfileMeta{
				Title:    title,
				Announce: strings.TrimSpace(parsed.Announce),
				Routing:  routingLinkFromTemplate(parsed),
				Found:    true,
			}
		}
	}
	return ProfileMeta{}
}

func (s *Service) GetOrCreateHPShortLink(ctx context.Context, token model.AccessToken, profile string, targetURL TargetURLFunc, newID ShortIDFunc, encrypt Encryptor) (model.ShortLink, error) {
	if !s.ProfileMeta(profile).Found {
		return model.ShortLink{}, store.ErrNotFound
	}
	if link, err := s.store.GetShortLinkByTokenProfile(token.ID, profile); err == nil && strings.TrimSpace(link.EncryptedURL) != "" {
		return link, nil
	}

	var lastErr error
	for i := 0; i < 4; i++ {
		id, err := newID()
		if err != nil {
			return model.ShortLink{}, err
		}
		rawURL := targetURL(id)
		encrypted, err := encrypt(ctx, rawURL)
		if err != nil {
			return model.ShortLink{}, err
		}
		link, err := s.store.CreateShortLink(model.ShortLink{
			ID:           id,
			TokenID:      token.ID,
			Profile:      profile,
			TargetURL:    rawURL,
			EncryptedURL: encrypted,
		})
		if err == nil {
			return link, nil
		}
		if existing, existingErr := s.store.GetShortLinkByTokenProfile(token.ID, profile); existingErr == nil && strings.TrimSpace(existing.EncryptedURL) != "" {
			return existing, nil
		}
		lastErr = err
	}
	return model.ShortLink{}, lastErr
}

func (s *Service) hy2Lines(token model.AccessToken, summary model.AccountSummary) []string {
	lines := []string{}
	for _, item := range s.activeSubscriptionConfigs(token, summary) {
		cfg := item.Config
		if cfg.Kind == model.ConfigHy2URI && strings.TrimSpace(cfg.Config) != "" {
			lines = append(lines, FormatSubscriptionLine(cfg, item.Node))
		}
	}
	return lines
}

type subscriptionConfig struct {
	Config model.IssuedConfig
	Node   model.Node
}

func (s *Service) activeSubscriptionConfigs(token model.AccessToken, summary model.AccountSummary) []subscriptionConfig {
	activeConnection := map[string]model.Node{}
	for _, item := range activeClientAccesses(token, summary) {
		activeConnection[item.Connection.ID] = item.Node
	}
	out := []subscriptionConfig{}
	for _, cfg := range summary.Configs {
		node, ok := activeConnection[cfg.ConnectionID]
		if cfg.Status == model.StatusActive && ok {
			out = append(out, subscriptionConfig{Config: cfg, Node: node})
		}
	}
	return out
}

func activeClientAccesses(token model.AccessToken, summary model.AccountSummary) []model.ConnectionSummary {
	out := []model.ConnectionSummary{}
	for _, item := range summary.Connections {
		if item.Connection.Status != model.StatusActive {
			continue
		}
		if token.ClientID != "" && item.Connection.ClientID != token.ClientID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func parseHPProfileTemplate(value string) hpProfileTemplate {
	var parsed hpProfileTemplate
	_ = json.Unmarshal([]byte(value), &parsed)
	return parsed
}

func routingLinkFromTemplate(template hpProfileTemplate) string {
	if template.Routing == nil || strings.TrimSpace(template.Routing.Name) == "" {
		return ""
	}
	return routingLinkFor(template.Routing.Name, template.Routing.ProxySites, template.Routing.ProxyIP)
}

func routingLinkFor(name string, proxySites, proxyIPs []string) string {
	raw, _ := json.Marshal(map[string]any{
		"Name":              name,
		"GlobalProxy":       "false",
		"RemoteDNSType":     "DoH",
		"RemoteDNSDomain":   "https://cloudflare-dns.com/dns-query",
		"RemoteDNSIP":       "1.1.1.1",
		"DomesticDNSType":   "DoH",
		"DomesticDNSDomain": "https://dns.google/dns-query",
		"DomesticDNSIP":     "8.8.8.8",
		"Geoipurl":          "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
		"Geositeurl":        "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat",
		"LastUpdated":       "",
		"DnsHosts": map[string]string{
			"cloudflare-dns.com": "1.1.1.1",
			"dns.google":         "8.8.8.8",
		},
		"DirectSites": []string{},
		"DirectIp": []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"169.254.0.0/16",
			"224.0.0.0/4",
			"255.255.255.255",
		},
		"ProxySites":     proxySites,
		"ProxyIp":        proxyIPs,
		"BlockSites":     []string{},
		"BlockIp":        []string{},
		"DomainStrategy": "IPIfNonMatch",
		"FakeDNS":        "false",
	})
	return "happ://routing/onadd/" + base64.StdEncoding.EncodeToString(raw)
}

func FormatSubscriptionLine(cfg model.IssuedConfig, node model.Node) string {
	line := strings.TrimSpace(cfg.Config)
	if line == "" {
		return ""
	}
	label := SubscriptionLabel(cfg.Name, node)
	if label == "" {
		return line
	}
	parsed, err := url.Parse(line)
	if err != nil || parsed.Scheme == "" {
		if strings.Contains(line, "#") {
			line = strings.SplitN(line, "#", 2)[0]
		}
		return line + "#" + url.QueryEscape(label)
	}
	parsed.Fragment = label
	return parsed.String()
}

func SubscriptionLabel(configName string, node model.Node) string {
	country := countryLabel(node.Region)
	cleanName := cleanSubscriptionName(configName, node)
	if country != "" && cleanName != "" {
		return country + " · " + cleanName
	}
	if country != "" {
		return country
	}
	return cleanName
}

func cleanSubscriptionName(configName string, node model.Node) string {
	name := strings.TrimSpace(configName)
	if name == "" {
		name = strings.TrimSpace(node.Name)
	}
	for _, remove := range []string{
		"hysteria2",
		"hysteria",
		"hy2",
		countryName(node.Region),
		countryCode(node.Region),
	} {
		name = removeLabelWord(name, remove)
	}
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, " -_·,")
	if name == "" {
		name = strings.TrimSpace(node.Name)
		for _, remove := range []string{"hysteria2", "hysteria", "hy2", countryName(node.Region), countryCode(node.Region)} {
			name = removeLabelWord(name, remove)
		}
		name = strings.Join(strings.Fields(name), " ")
		name = strings.Trim(name, " -_·,")
	}
	return name
}

func removeLabelWord(value, word string) string {
	word = strings.TrimSpace(word)
	if word == "" {
		return value
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_' || r == '·' || r == ',' || r == '/'
	})
	out := []string{}
	for _, field := range fields {
		if !strings.EqualFold(field, word) {
			out = append(out, field)
		}
	}
	return strings.Join(out, " ")
}

func countryLabel(region string) string {
	code, name := countryCodeName(region)
	if code == "" && name == "" {
		return ""
	}
	if code == "" {
		return name
	}
	flag := countryFlag(code)
	if name == "" {
		return strings.TrimSpace(flag + " " + code)
	}
	return strings.TrimSpace(flag + " " + name)
}

func countryName(region string) string {
	_, name := countryCodeName(region)
	return name
}

func countryCode(region string) string {
	code, _ := countryCodeName(region)
	return code
}

func countryCodeName(region string) (string, string) {
	value := strings.TrimSpace(region)
	if value == "" {
		return "", ""
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '/'
	})
	if len(parts) > 1 {
		if code, name := countryCodeName(parts[0]); code != "" || name != "" {
			return code, name
		}
	}
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, " ", ""), "_", ""))
	countries := map[string][2]string{
		"am":           {"AM", "Armenia"},
		"armenia":      {"AM", "Armenia"},
		"de":           {"DE", "Germany"},
		"germany":      {"DE", "Germany"},
		"ee":           {"EE", "Estonia"},
		"estonia":      {"EE", "Estonia"},
		"fi":           {"FI", "Finland"},
		"finland":      {"FI", "Finland"},
		"fr":           {"FR", "France"},
		"france":       {"FR", "France"},
		"nl":           {"NL", "Netherlands"},
		"netherlands":  {"NL", "Netherlands"},
		"ru":           {"RU", "Russia"},
		"russia":       {"RU", "Russia"},
		"tr":           {"TR", "Turkey"},
		"turkey":       {"TR", "Turkey"},
		"us":           {"US", "United States"},
		"usa":          {"US", "United States"},
		"unitedstates": {"US", "United States"},
	}
	if item, ok := countries[key]; ok {
		return item[0], item[1]
	}
	code := strings.ToUpper(value)
	if len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
		return code, code
	}
	return "", value
}

func countryFlag(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 {
		return ""
	}
	runes := []rune(code)
	if runes[0] < 'A' || runes[0] > 'Z' || runes[1] < 'A' || runes[1] > 'Z' {
		return ""
	}
	return string([]rune{0x1F1E6 + runes[0] - 'A', 0x1F1E6 + runes[1] - 'A'})
}
