package util

import "time"

func UnixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

func UnixMilliOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return UnixMilli(t)
}

func FromUnixMilli(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
