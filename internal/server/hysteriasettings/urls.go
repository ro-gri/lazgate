package hysteriasettings

import "net/url"

func redactURL(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return raw
	}
	u.User = nil
	return u.String()
}
func restoreURL(raw, old string) string {
	if raw == redactURL(old) {
		return old
	}
	return raw
}
