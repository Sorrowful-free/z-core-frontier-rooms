package httplimits

import (
	"sync"
	"time"
)

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]limitWindow
}

type limitWindow struct {
	count   int
	resetAt time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		windows: make(map[string]limitWindow),
	}
}

// allow reports whether key may proceed: limit requests per window.
// limit <= 0 — без ограничений.
func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	w, ok := l.windows[key]
	if !ok || !now.Before(w.resetAt) {
		l.windows[key] = limitWindow{
			count:   1,
			resetAt: now.Add(window),
		}
		return true
	}

	if w.count >= limit {
		return false
	}

	w.count++
	l.windows[key] = w
	return true
}
