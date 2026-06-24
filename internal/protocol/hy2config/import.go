package hy2config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// FromRuntime imports editable settings without importing auth or certificate material.
func FromRuntime(runtime map[string]any) (Config, error) {
	c := Defaults()
	c.ProtectInternal = false
	// Hysteria accepts integer bandwidth as bytes/s, but the form uses explicit bit/s units.
	if bandwidth, ok := runtime["bandwidth"].(map[string]any); ok {
		for _, key := range []string{"up", "down"} {
			if value, exists := bandwidth[key]; exists {
				if _, isString := value.(string); !isString {
					n, err := strconv.ParseUint(fmt.Sprint(value), 10, 64)
					if err != nil || n > ^uint64(0)/8 {
						return c, fmt.Errorf("invalid bandwidth")
					}
					if n == 0 {
						bandwidth[key] = "0"
					} else {
						bandwidth[key] = fmt.Sprintf("%d bps", n*8)
					}
				}
			}
		}
	}
	raw, err := json.Marshal(runtime)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if listen, ok := runtime["listen"].(string); ok {
		var port int
		if _, err = fmt.Sscanf(listen, ":%d", &port); err == nil {
			c.ListenPort = port
		}
	}
	object := func(key string) map[string]any { m, _ := runtime[key].(map[string]any); return m }
	if m := object("obfs"); m != nil {
		typ, _ := m["type"].(string)
		c.Obfs.Type = typ
		opts, _ := m[typ].(map[string]any)
		raw, _ = json.Marshal(opts)
		if err = json.Unmarshal(raw, &c.Obfs); err != nil {
			return c, err
		}
	}
	if m := object("resolver"); m != nil {
		typ, _ := m["type"].(string)
		c.Resolver.Type = typ
		opts, _ := m[typ].(map[string]any)
		raw, _ = json.Marshal(opts)
		if err = json.Unmarshal(raw, &c.Resolver); err != nil {
			return c, err
		}
	}
	if m := object("masquerade"); m != nil {
		typ, _ := m["type"].(string)
		c.Masquerade.Type = typ
		opts, _ := m[typ].(map[string]any)
		raw, _ = json.Marshal(opts)
		if err = json.Unmarshal(raw, &c.Masquerade); err != nil {
			return c, err
		}
	}
	if m := object("acl"); m != nil {
		if _, ok := m["file"]; ok {
			return c, fmt.Errorf("external ACL files must be imported before editing")
		}
		if rules, ok := m["inline"].([]any); ok {
			c.ACLRules = nil
			for _, v := range rules {
				rule, ok := v.(string)
				if !ok {
					return c, fmt.Errorf("invalid ACL rule")
				}
				if strings.HasPrefix(rule, "reject(geoip:") && strings.HasSuffix(rule, ")") {
					c.BlockedCountries = append(c.BlockedCountries, strings.TrimSuffix(strings.TrimPrefix(rule, "reject(geoip:"), ")"))
				} else {
					c.ACLRules = append(c.ACLRules, rule)
				}
			}
		}
	}
	if list, ok := runtime["outbounds"].([]any); ok {
		c.Outbounds = nil
		for _, entry := range list {
			m, ok := entry.(map[string]any)
			if !ok {
				return c, fmt.Errorf("invalid outbound")
			}
			o := Outbound{Mode: "auto"}
			raw, _ = json.Marshal(m)
			if err = json.Unmarshal(raw, &o); err != nil {
				return c, err
			}
			opts, _ := m[o.Type].(map[string]any)
			if mode, ok := opts["mode"]; ok {
				opts["mode"] = fmt.Sprint(mode)
			}
			raw, _ = json.Marshal(opts)
			if err = json.Unmarshal(raw, &o); err != nil {
				return c, err
			}
			c.Outbounds = append(c.Outbounds, o)
		}
	}
	return c, Validate(c)
}
