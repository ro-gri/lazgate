package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicateConnection = errors.New("connection already exists for account, client and node")

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func now() time.Time {
	return time.Now().UTC()
}
