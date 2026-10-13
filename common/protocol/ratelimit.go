package protocol

import (
	"context"
	"sync"

	"golang.org/x/time/rate"
)

const rateLimitBurst = 64 * 1024

type RateLimiter struct {
	limiter *rate.Limiter
}

func NewRateLimiter(bytesPerSecond uint64) *RateLimiter {
	if bytesPerSecond == 0 {
		return nil
	}
	return &RateLimiter{limiter: rate.NewLimiter(rate.Limit(bytesPerSecond), rateLimitBurst)}
}

func (l *RateLimiter) SetRate(bytesPerSecond uint64) {
	if l == nil {
		return
	}
	limit := rate.Limit(bytesPerSecond)
	if bytesPerSecond == 0 {
		limit = rate.Inf
	}
	l.limiter.SetLimit(limit)
}

func (l *RateLimiter) Wait(ctx context.Context, size int) error {
	if l == nil {
		return nil
	}
	for size > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := min(size, rateLimitBurst)
		if err := l.limiter.WaitN(ctx, chunk); err != nil {
			return err
		}
		size -= chunk
	}
	return nil
}

type limiterEntry struct {
	limiter *RateLimiter
	refs    int
}

type limiterRegistry struct {
	mu     sync.Mutex
	byUser map[string]*limiterEntry
}

var globalLimiterRegistry = newLimiterRegistry()

func newLimiterRegistry() *limiterRegistry {
	return &limiterRegistry{byUser: make(map[string]*limiterEntry)}
}

func (r *limiterRegistry) Update(email, direction string, bytesPerSecond uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry := r.byUser[direction+":"+email]; entry != nil {
		entry.limiter.SetRate(bytesPerSecond)
	}
}

func (r *limiterRegistry) Acquire(ctx context.Context, email, direction string, bytesPerSecond uint64) *RateLimiter {
	if bytesPerSecond == 0 {
		return nil
	}
	key := direction + ":" + email
	r.mu.Lock()
	entry := r.byUser[key]
	if entry == nil {
		entry = &limiterEntry{limiter: NewRateLimiter(bytesPerSecond)}
		r.byUser[key] = entry
	} else {
		entry.limiter.SetRate(bytesPerSecond)
	}
	entry.refs++
	r.mu.Unlock()
	context.AfterFunc(ctx, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		entry.refs--
		if entry.refs == 0 && r.byUser[key] == entry {
			delete(r.byUser, key)
		}
	})
	return entry.limiter
}
