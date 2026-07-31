package guardrails

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimiter limits request frequency with a token bucket.
// Refills rate tokens/second, burst capacity. Applies to input.
type RateLimiter struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64 // tokens per second
	burst  float64
}

func NewRateLimiter(rate, burst float64) *RateLimiter {
	if rate <= 0 {
		rate = 1
	}
	if burst < rate {
		burst = rate
	}
	return &RateLimiter{
		tokens: burst,
		last:   time.Now(),
		rate:   rate,
		burst:  burst,
	}
}

// Allow takes one token when available.
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.tokens += now.Sub(r.last).Seconds() * r.rate
	r.last = now
	if r.tokens > r.burst {
		r.tokens = r.burst
	}
	if r.tokens < 1 {
		return false
	}
	r.tokens--
	return true
}

func (r *RateLimiter) ValidateInput(_ context.Context, _ string) error {
	if !r.Allow() {
		return fmt.Errorf("rate limit exceeded")
	}
	return nil
}

func (r *RateLimiter) ValidateOutput(_ context.Context, _ string) error {
	return nil
}
