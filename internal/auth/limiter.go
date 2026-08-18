package auth

import (
	"sync"
	"time"
)

type LimitResult struct {
	Allowed    bool
	RetryAfter time.Duration
}

type Limiter interface {
	Allow(key string, now time.Time) LimitResult
	Reset(key string)
}

type MemoryLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]limitEntry
}

type limitEntry struct {
	count   int
	started time.Time
}

func NewMemoryLimiter(limit int, window time.Duration) *MemoryLimiter {
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = time.Minute
	}
	return &MemoryLimiter{limit: limit, window: window, entries: map[string]limitEntry{}}
}

func (l *MemoryLimiter) Allow(key string, now time.Time) LimitResult {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	if entry.started.IsZero() || now.Sub(entry.started) >= l.window {
		entry = limitEntry{started: now}
	}
	if entry.count >= l.limit {
		return LimitResult{RetryAfter: l.window - now.Sub(entry.started)}
	}
	entry.count++
	l.entries[key] = entry
	return LimitResult{Allowed: true}
}

func (l *MemoryLimiter) Reset(key string) { l.mu.Lock(); delete(l.entries, key); l.mu.Unlock() }
