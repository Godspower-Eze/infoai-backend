package httpapi

import (
	"sync"
	"time"
)

type windowEntry struct {
	started time.Time
	count   int
}

type fixedWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	keys   map[string]windowEntry
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{limit: limit, window: window, keys: make(map[string]windowEntry)}
}

func (limiter *fixedWindowLimiter) Allow(key string, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for storedKey, entry := range limiter.keys {
		if now.Sub(entry.started) >= limiter.window {
			delete(limiter.keys, storedKey)
		}
	}
	entry, exists := limiter.keys[key]
	if !exists {
		limiter.keys[key] = windowEntry{started: now, count: 1}
		return true
	}
	if entry.count >= limiter.limit {
		return false
	}
	entry.count++
	limiter.keys[key] = entry
	return true
}
