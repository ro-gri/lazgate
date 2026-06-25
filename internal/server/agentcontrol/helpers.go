package agentcontrol

import (
	"net/url"
	"strings"
	"time"
)

func msTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func passwordFromHy2URI(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "hy2://") {
		raw = "hysteria2://" + strings.TrimPrefix(raw, "hy2://")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return ""
	}
	if password, ok := u.User.Password(); ok {
		return password
	}
	return u.User.Username()
}
