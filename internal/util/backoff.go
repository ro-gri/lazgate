package util

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	mathrand "math/rand"
	"sync"
	"time"
)

type Backoff struct {
	min, max time.Duration
	attempts int
	random   *mathrand.Rand
	mu       sync.Mutex
}

func NewBackoff(minDelay, maxDelay time.Duration) *Backoff {
	if minDelay <= 0 {
		minDelay = time.Second
	}
	if maxDelay < minDelay {
		maxDelay = minDelay
	}
	return &Backoff{
		min:    minDelay,
		max:    maxDelay,
		random: mathrand.New(mathrand.NewSource(randomSeed())),
	}
}

func (b *Backoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.attempts++
	progress := math.Min(float64(b.attempts)/10, 1)
	ceil := b.min + time.Duration(progress*progress*float64(b.max-b.min))
	ceil = max(ceil, b.min)
	ceil = min(ceil, b.max)
	floor := ceil / 2
	floor = max(floor, b.min)
	if floor >= ceil {
		return ceil
	}
	return floor + time.Duration(b.random.Int63n(int64(ceil-floor)+1))
}

func (b *Backoff) Reset() {
	b.mu.Lock()
	b.attempts = 0
	b.mu.Unlock()
}

func randomSeed() int64 {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return int64(binary.LittleEndian.Uint64(raw[:]))
	}
	return time.Now().UnixNano()
}
