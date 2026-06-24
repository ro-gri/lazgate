package store

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("configuration changed concurrently")
var ErrDuplicateConnection = errors.New("connection already exists for account, node and name")

type scanner interface {
	Scan(dest ...any) error
}

func NewID(_ string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func now() time.Time {
	return time.Now().UTC()
}
