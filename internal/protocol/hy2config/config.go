// Package hy2config describes editable Hysteria settings, excluding LazGate integration and TLS secrets.
package hy2config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
)

type Config struct {
	ListenPort            int        `json:"listen_port"`
	Obfs                  Obfs       `json:"obfs"`
	Masquerade            Masquerade `json:"masquerade"`
	QUIC                  QUIC       `json:"quic"`
	Bandwidth             Bandwidth  `json:"bandwidth"`
	IgnoreClientBandwidth bool       `json:"ignoreClientBandwidth"`
	Congestion            Congestion `json:"congestion"`
	DisableUDP            bool       `json:"disableUDP"`
	UDPIdleTimeout        string     `json:"udpIdleTimeout"`
	SpeedTest             bool       `json:"speedTest"`
	Resolver              Resolver   `json:"resolver"`
	Sniff                 Sniff      `json:"sniff"`
	ProtectInternal       bool       `json:"protect_internal"`
	BlockedCountries      []string   `json:"blocked_countries"`
	ACLRules              []string   `json:"acl_rules"`
	Outbounds             []Outbound `json:"outbounds"`
}
type Obfs struct {
	Type          string `json:"type"`
	Password      string `json:"password"`
	MinPacketSize int    `json:"minPacketSize"`
	MaxPacketSize int    `json:"maxPacketSize"`
}
type QUIC struct {
	InitStreamReceiveWindow uint64 `json:"initStreamReceiveWindow"`
	MaxStreamReceiveWindow  uint64 `json:"maxStreamReceiveWindow"`
	InitConnReceiveWindow   uint64 `json:"initConnReceiveWindow"`
	MaxConnReceiveWindow    uint64 `json:"maxConnReceiveWindow"`
	MaxIdleTimeout          string `json:"maxIdleTimeout"`
	MaxIncomingStreams      int64  `json:"maxIncomingStreams"`
	DisablePathMTUDiscovery bool   `json:"disablePathMTUDiscovery"`
	DisableStatelessReset   bool   `json:"disableStatelessReset"`
}
type Bandwidth struct {
	Up                      string `json:"up"`
	Down                    string `json:"down"`
	DisableLossCompensation bool   `json:"disableLossCompensation"`
}
type Congestion struct {
	Type       string `json:"type"`
	BBRProfile string `json:"bbrProfile"`
}
type Resolver struct {
	Type     string `json:"type"`
	Addr     string `json:"addr"`
	Timeout  string `json:"timeout"`
	SNI      string `json:"sni"`
	Insecure bool   `json:"insecure"`
}
type Sniff struct {
	Enable        bool   `json:"enable"`
	Timeout       string `json:"timeout"`
	RewriteDomain bool   `json:"rewriteDomain"`
	TCPPorts      string `json:"tcpPorts"`
	UDPPorts      string `json:"udpPorts"`
}
type Masquerade struct {
	Type        string            `json:"type"`
	URL         string            `json:"url"`
	RewriteHost bool              `json:"rewriteHost"`
	Insecure    bool              `json:"insecure"`
	XForwarded  bool              `json:"xForwarded"`
	Dir         string            `json:"dir"`
	Content     string            `json:"content"`
	Headers     map[string]string `json:"headers"`
	StatusCode  int               `json:"statusCode"`
	ListenHTTP  string            `json:"listenHTTP"`
	ListenHTTPS string            `json:"listenHTTPS"`
	ForceHTTPS  bool              `json:"forceHTTPS"`
}
type Outbound struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Mode       string `json:"mode"`
	BindIPv4   string `json:"bindIPv4"`
	BindIPv6   string `json:"bindIPv6"`
	BindDevice string `json:"bindDevice"`
	FastOpen   bool   `json:"fastOpen"`
	Addr       string `json:"addr"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	URL        string `json:"url"`
	Insecure   bool   `json:"insecure"`
}

func Defaults() Config {
	return Config{ListenPort: 443, Obfs: Obfs{Type: "off", MinPacketSize: 512, MaxPacketSize: 1200},
		Masquerade: Masquerade{Type: "off", RewriteHost: true, StatusCode: 200, Headers: map[string]string{}},
		QUIC:       QUIC{InitStreamReceiveWindow: 8388608, MaxStreamReceiveWindow: 8388608, InitConnReceiveWindow: 20971520, MaxConnReceiveWindow: 20971520, MaxIdleTimeout: "30s", MaxIncomingStreams: 1024},
		Bandwidth:  Bandwidth{Up: "0", Down: "0"}, Congestion: Congestion{Type: "bbr", BBRProfile: "standard"}, UDPIdleTimeout: "60s",
		Resolver: Resolver{Type: "system", Timeout: "10s"}, Sniff: Sniff{Timeout: "2s", TCPPorts: "all", UDPPorts: "all"},
		ProtectInternal: true, BlockedCountries: []string{}, ACLRules: []string{}, Outbounds: []Outbound{{Name: "direct", Type: "direct", Mode: "auto"}}}
}

func Decode(raw string) (Config, error) {
	c := Defaults()
	if raw == "" {
		return c, nil
	}
	err := json.Unmarshal([]byte(raw), &c)
	return c, err
}

var bandwidthPattern = regexp.MustCompile(`(?i)^(0|\d+\s*(bps|b|kbps|kb|k|mbps|mb|m|gbps|gb|g|tbps|tb|t))$`)

func Domain(value string) bool {
	if value == "localhost" {
		return true
	}
	if len(value) > 253 || net.ParseIP(value) != nil {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return strings.Contains(value, ".")
}
func Validate(c Config) error {
	if c.ListenPort < 1 || c.ListenPort > 65535 {
		return errors.New("Порт должен быть от 1 до 65535")
	}
	switch c.Obfs.Type {
	case "off":
	case "salamander", "gecko":
		if len(c.Obfs.Password) < 4 {
			return errors.New("Пароль обфускации должен содержать минимум 4 байта")
		}
	default:
		return errors.New("Неизвестный тип обфускации")
	}
	if c.Obfs.Type == "gecko" && (c.Obfs.MinPacketSize < 1 || c.Obfs.MaxPacketSize < c.Obfs.MinPacketSize || c.Obfs.MaxPacketSize > 2048) {
		return errors.New("Размеры Gecko: 1 ≤ минимальный ≤ максимальный ≤ 2048")
	}
	q := c.QUIC
	if q.InitStreamReceiveWindow < 16384 || q.InitConnReceiveWindow < 16384 || q.MaxStreamReceiveWindow < q.InitStreamReceiveWindow || q.MaxConnReceiveWindow < q.InitConnReceiveWindow || q.MaxStreamReceiveWindow > 1<<60 || q.MaxConnReceiveWindow > 1<<60 || q.MaxIncomingStreams < 8 || q.MaxIncomingStreams > 1<<60 {
		return errors.New("Некорректные окна QUIC или число потоков (минимум 8)")
	}
	for name, value := range map[string]string{"QUIC": q.MaxIdleTimeout, "UDP": c.UDPIdleTimeout, "DNS": c.Resolver.Timeout, "Sniffing": c.Sniff.Timeout} {
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return fmt.Errorf("Некорректный таймаут %s", name)
		}
	}
	idle, _ := time.ParseDuration(q.MaxIdleTimeout)
	if idle < 4*time.Second || idle > 120*time.Second {
		return errors.New("Таймаут QUIC должен быть от 4 до 120 секунд")
	}
	if !bandwidthPattern.MatchString(c.Bandwidth.Up) || !bandwidthPattern.MatchString(c.Bandwidth.Down) {
		return errors.New("Скорость: неотрицательное число с единицей, например 200 mbps или 0")
	}
	if c.Congestion.Type != "bbr" && c.Congestion.Type != "reno" {
		return errors.New("Алгоритм должен быть bbr или reno")
	}
	if c.Congestion.BBRProfile != "standard" && c.Congestion.BBRProfile != "conservative" && c.Congestion.BBRProfile != "aggressive" {
		return errors.New("Некорректный профиль BBR")
	}
	switch c.Resolver.Type {
	case "system":
	case "udp", "tcp", "tls", "https":
		if err := hostPort(c.Resolver.Addr); err != nil {
			return fmt.Errorf("DNS: %w", err)
		}
	default:
		return errors.New("Неизвестный тип DNS")
	}
	if err := ports(c.Sniff.TCPPorts); err != nil {
		return err
	}
	if err := ports(c.Sniff.UDPPorts); err != nil {
		return err
	}
	for _, code := range c.BlockedCountries {
		r, e := language.ParseRegion(strings.ToUpper(code))
		if e != nil || len(code) != 2 || !r.IsCountry() {
			return errors.New("Некорректный код страны ACL")
		}
	}
	switch c.Masquerade.Type {
	case "off":
	case "proxy":
		if !httpURL(c.Masquerade.URL) {
			return errors.New("Укажите HTTP/HTTPS URL маскировки")
		}
	case "file":
		if !filepath.IsAbs(c.Masquerade.Dir) {
			return errors.New("Каталог маскировки должен быть абсолютным")
		}
	case "string":
		if c.Masquerade.StatusCode < 200 || c.Masquerade.StatusCode > 599 {
			return errors.New("HTTP-код должен быть 200–599")
		}
	default:
		return errors.New("Неизвестный режим маскировки")
	}
	for k, v := range c.Masquerade.Headers {
		if k == "" || strings.ContainsAny(k+v, "\r\n") || http.CanonicalHeaderKey(k) == "" {
			return errors.New("Некорректный HTTP-заголовок")
		}
	}
	for _, v := range []string{c.Masquerade.ListenHTTP, c.Masquerade.ListenHTTPS} {
		if v != "" {
			if err := hostPort(v); err != nil {
				return fmt.Errorf("Адрес маскировки: %w", err)
			}
		}
	}
	if c.Masquerade.Type != "off" {
		if c.Masquerade.ListenHTTP != "" && c.Masquerade.ListenHTTPS == "" {
			return errors.New("Для HTTP-маскировки также нужен HTTPS адрес:порт")
		}
		if c.Masquerade.ForceHTTPS && (c.Masquerade.ListenHTTP == "" || c.Masquerade.ListenHTTPS == "") {
			return errors.New("Для перенаправления нужны HTTP и HTTPS адреса")
		}
	}
	names := map[string]bool{}
	if len(c.Outbounds) == 0 {
		return errors.New("Нужен хотя бы один исходящий маршрут")
	}
	for _, o := range c.Outbounds {
		if o.Name == "" || names[o.Name] {
			return errors.New("Имена выходов должны быть непустыми и уникальными")
		}
		names[o.Name] = true
		switch o.Type {
		case "direct":
			if !strings.Contains("|auto|4|6|46|64|", "|"+o.Mode+"|") {
				return errors.New("Некорректный IP-режим")
			}
			if o.BindDevice != "" && (o.BindIPv4 != "" || o.BindIPv6 != "") {
				return errors.New("Выберите IP или интерфейс для привязки")
			}
			for _, v := range []string{o.BindIPv4, o.BindIPv6} {
				if v != "" && net.ParseIP(v) == nil {
					return errors.New("Некорректный исходящий IP")
				}
			}
		case "socks5":
			if err := hostPort(o.Addr); err != nil {
				return err
			}
		case "http":
			if !httpURL(o.URL) {
				return errors.New("Некорректный URL HTTP-прокси")
			}
		default:
			return errors.New("Неизвестный тип выхода")
		}
	}
	return nil
}
func hostPort(v string) error {
	_, p, e := net.SplitHostPort(v)
	n, err := strconv.Atoi(p)
	if e != nil || err != nil || n < 1 || n > 65535 {
		return errors.New("Ожидается адрес:порт, порт 1–65535")
	}
	return nil
}
func httpURL(v string) bool {
	u, e := url.Parse(v)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}
func ports(v string) error {
	if v == "all" {
		return nil
	}
	for _, s := range strings.Split(v, ",") {
		a := strings.Split(strings.TrimSpace(s), "-")
		if len(a) > 2 {
			return errors.New("Некорректные порты sniffing")
		}
		last := 0
		for _, p := range a {
			n, e := strconv.Atoi(p)
			if e != nil || n < 1 || n > 65535 || n < last {
				return errors.New("Некорректные порты sniffing")
			}
			last = n
		}
	}
	return nil
}

// Runtime overlays only editable keys. auth, trafficStats, TLS/ACME and unknown keys remain intact.
func (c Config) Runtime(current map[string]any, domain string) (map[string]any, error) {
	if current == nil {
		current = map[string]any{}
	}
	if err := Validate(c); err != nil {
		return nil, err
	}
	current["listen"] = fmt.Sprintf(":%d", c.ListenPort)
	current["quic"] = c.QUIC
	up, down := c.Bandwidth.Up, c.Bandwidth.Down
	if c.Bandwidth.Up == "0" {
		up = "0 bps"
	}
	if c.Bandwidth.Down == "0" {
		down = "0 bps"
	}
	current["bandwidth"] = map[string]any{"up": up, "down": down, "disableLossCompensation": c.Bandwidth.DisableLossCompensation}
	current["congestion"] = c.Congestion
	current["ignoreClientBandwidth"] = c.IgnoreClientBandwidth
	current["disableUDP"] = c.DisableUDP
	current["udpIdleTimeout"] = c.UDPIdleTimeout
	current["speedTest"] = c.SpeedTest
	delete(current, "obfs")
	if c.Obfs.Type != "off" {
		opts := map[string]any{"password": c.Obfs.Password}
		if c.Obfs.Type == "gecko" {
			opts["minPacketSize"] = c.Obfs.MinPacketSize
			opts["maxPacketSize"] = c.Obfs.MaxPacketSize
		}
		current["obfs"] = map[string]any{"type": c.Obfs.Type, c.Obfs.Type: opts}
	}
	delete(current, "resolver")
	if c.Resolver.Type != "system" {
		current["resolver"] = map[string]any{"type": c.Resolver.Type, c.Resolver.Type: map[string]any{"addr": c.Resolver.Addr, "timeout": c.Resolver.Timeout, "sni": c.Resolver.SNI, "insecure": c.Resolver.Insecure}}
	}
	current["sniff"] = c.Sniff
	rules := []string{}
	if c.ProtectInternal {
		for _, p := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "::/128", "::1/128", "fc00::/7", "fe80::/10", "::ffff:0:0/96"} {
			rules = append(rules, "reject("+p+")")
		}
	}
	for _, country := range c.BlockedCountries {
		rules = append(rules, "reject(geoip:"+strings.ToLower(country)+")")
	}
	rules = append(rules, c.ACLRules...)
	acl, _ := current["acl"].(map[string]any)
	if acl == nil {
		acl = map[string]any{}
	}
	delete(acl, "file")
	acl["inline"] = rules
	current["acl"] = acl
	out := []map[string]any{}
	for _, o := range c.Outbounds {
		opts := map[string]any{}
		switch o.Type {
		case "direct":
			opts = map[string]any{"mode": o.Mode, "fastOpen": o.FastOpen}
			if o.BindDevice != "" {
				opts["bindDevice"] = o.BindDevice
			}
			if o.BindIPv4 != "" {
				opts["bindIPv4"] = o.BindIPv4
			}
			if o.BindIPv6 != "" {
				opts["bindIPv6"] = o.BindIPv6
			}
		case "socks5":
			opts = map[string]any{"addr": o.Addr, "username": o.Username, "password": o.Password}
		case "http":
			opts = map[string]any{"url": o.URL, "insecure": o.Insecure}
		}
		out = append(out, map[string]any{"name": o.Name, "type": o.Type, o.Type: opts})
	}
	current["outbounds"] = out
	delete(current, "masquerade")
	m := c.Masquerade
	if m.Type != "off" {
		current["masquerade"] = map[string]any{"type": m.Type, "proxy": map[string]any{"url": m.URL, "rewriteHost": m.RewriteHost, "insecure": m.Insecure, "xForwarded": m.XForwarded}, "file": map[string]any{"dir": m.Dir}, "string": map[string]any{"content": m.Content, "headers": m.Headers, "statusCode": m.StatusCode}, "listenHTTP": m.ListenHTTP, "listenHTTPS": m.ListenHTTPS, "forceHTTPS": m.ForceHTTPS}
	}
	if acme, ok := current["acme"].(map[string]any); ok {
		acme["domains"] = []string{domain}
	}
	return current, nil
}
