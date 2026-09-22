package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a token-bucket rate limiter. It allows up to rate requests per
// second, with a burst capacity. When the bucket is empty, requests are
// rejected (429) and the caller should honor Retry-After.
type Limiter struct {
	mu       sync.Mutex
	rate     float64 // tokens per second
	burst    float64 // max tokens (bucket capacity)
	tokens   float64
	last     time.Time
}

// New returns a Limiter allowing rate requests/sec with the given burst.
func New(rate, burst float64) *Limiter {
	if rate <= 0 {
		rate = 1
	}
	if burst <= 0 {
		burst = rate
	}
	return &Limiter{
		rate:   rate,
		burst:  burst,
		tokens: burst,
		last:   time.Now(),
	}
}

// Allow reports whether a request is permitted now. If not, retryAfter is the
// duration the caller should wait before retrying.
func (l *Limiter) Allow() (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	// Refill tokens based on elapsed time.
	elapsed := now.Sub(l.last).Seconds()
	l.tokens += elapsed * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.last = now

	if l.tokens >= 1 {
		l.tokens--
		return true, 0
	}
	// Time until one token is available.
	wait := time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
	if wait < time.Second {
		wait = time.Second
	}
	return false, wait
}