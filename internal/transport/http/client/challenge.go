package client

import (
	"crypto/subtle"
	"strings"
)

func challengeMatches(username, challenge string) bool {
	want := challengeForAccountname(username)
	got := strings.ToLower(strings.TrimSpace(challenge))
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func challengeForAccountname(username string) string {
	username = strings.ToLower(strings.TrimSpace(username))
	runes := []rune(username)
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[:2]) + string(runes[len(runes)-2:])
}
