package httplimits

import (
	"sync"
	"time"
)

// rateLimitSweepInterval — how often allow evicts expired windows (lazy, no goroutine).
const rateLimitSweepInterval = time.Minute

type rateLimiter struct {
	mu        sync.Mutex
	windows   map[string]limitWindow
	lastSweep time.Time
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
// Fixed window: at the start of a new window the key gets the full limit
// again, so a burst of up to 2×limit is possible at window boundaries.
// limit <= 0 — unlimited.
func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.evictExpired(now)

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

// evictExpired removes windows with expired resetAt at most once per rateLimitSweepInterval.
// Called under l.mu; keeps windows from growing as keys (IPs) rotate.
func (l *rateLimiter) evictExpired(now time.Time) {
	if now.Sub(l.lastSweep) < rateLimitSweepInterval {
		return
	}
	l.lastSweep = now
	for key, w := range l.windows {
		if !now.Before(w.resetAt) {
			delete(l.windows, key)
		}
	}
}
