package hysteriaagent

import (
	"net/url"
	"strings"
)

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
	username := u.User.Username()
	if _, password, ok := strings.Cut(username, ":"); ok {
		return password
	}
	return username
}
