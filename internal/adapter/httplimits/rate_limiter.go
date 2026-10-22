package httplimits

import (
	"sync"
	"time"
)

// rateLimitSweepInterval — как часто allow вычищает просроченные окна (lazy, без горутины).
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
// Fixed window: в начале нового окна ключ снова получает полный лимит,
// поэтому на границе окон возможен burst до 2×limit.
// limit <= 0 — без ограничений.
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

// evictExpired удаляет окна с истёкшим resetAt не чаще rateLimitSweepInterval.
// Вызывается под l.mu; удерживает рост windows при ротации ключей (IP).
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
