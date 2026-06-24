package util

import (
	"testing"
	"time"
)

func TestBackoffStaysWithinBounds(t *testing.T) {
	const (
		minimum = time.Second
		maximum = 5 * time.Minute
	)
	backoff := NewBackoff(minimum, maximum)
	for range 100 {
		delay := backoff.Next()
		if delay < minimum || delay > maximum {
			t.Fatalf("delay %s outside [%s, %s]", delay, minimum, maximum)
		}
	}
}

func TestBackoffResetReturnsToInitialRange(t *testing.T) {
	const (
		minimum = time.Second
		maximum = 5 * time.Minute
	)
	backoff := NewBackoff(minimum, maximum)
	for range 20 {
		_ = backoff.Next()
	}
	backoff.Reset()

	firstAttemptCeiling := minimum + time.Duration(0.01*float64(maximum-minimum))
	if delay := backoff.Next(); delay > firstAttemptCeiling {
		t.Fatalf("first delay after Reset() = %s, want <= %s", delay, firstAttemptCeiling)
	}
}
