// Package redisbreaker wraps Redis calls in a circuit breaker so that a Redis
// outage fails fast (no per-request timeout stalls) instead of dragging down
// every request. Callers treat an unavailable result as "skip Redis".
package redisbreaker

import (
	"time"

	"github.com/sony/gobreaker"
)

// Breaker guards Redis operations with a gobreaker circuit breaker.
type Breaker struct {
	cb *gobreaker.CircuitBreaker
}

// New builds a Breaker that trips Open after maxFailures consecutive failures
// and stays Open for openTimeout before allowing a half-open probe.
func New(maxFailures int, openTimeout time.Duration) *Breaker {
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:    "redis",
		Timeout: openTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(maxFailures)
		},
	})
	return &Breaker{cb: cb}
}

// Do runs fn through the breaker. When the breaker is Open it returns
// gobreaker.ErrOpenState without calling fn.
func (b *Breaker) Do(fn func() (any, error)) (any, error) {
	return b.cb.Execute(fn)
}

// IsUnavailable reports whether err means Redis is effectively unavailable —
// the breaker is rejecting calls (gobreaker.ErrOpenState/ErrTooManyRequests) or
// the underlying op failed. Any non-nil error qualifies, so callers fail open.
func IsUnavailable(err error) bool {
	return err != nil
}
