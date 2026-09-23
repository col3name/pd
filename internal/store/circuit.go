package store

import (
	"sync"
	"time"
)

// Breaker is a simple circuit breaker. It opens after `failures` consecutive
// failures and stays open for `cooldown`, then allows a probe request.
type Breaker struct {
	mu        sync.Mutex
	failures  int
	threshold int
	cooldown  time.Duration
	openUntil time.Time
}

func NewBreaker(failures int, cooldown time.Duration) *Breaker {
	if failures <= 0 {
		failures = 1
	}
	if cooldown <= 0 {
		cooldown = time.Second
	}
	return &Breaker{threshold: failures, cooldown: cooldown}
}

// Allow reports whether a call to the guarded resource is permitted now.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures >= b.threshold && !time.Now().After(b.openUntil) {
		return false
	}
	return true
}

// Success resets the failure count.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
}

// Failure increments the failure count and opens the breaker when the
// threshold is reached.
func (b *Breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = time.Now().Add(b.cooldown)
	}
}

// State reports whether the breaker is closed (up).
func (b *Breaker) State() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures < b.threshold || time.Now().After(b.openUntil)
}
