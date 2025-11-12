package middleware

import (
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	StateClosed   CircuitState = iota // normal operation
	StateOpen                         // blocking requests
	StateHalfOpen                     // testing with a single request
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern.
// It tracks consecutive failures and opens the circuit when a threshold is
// reached, preventing further requests until a timeout elapses.
type CircuitBreaker struct {
	maxFailures int
	timeout     time.Duration

	mu          sync.Mutex
	state       CircuitState
	failures    int
	lastFailure time.Time
}

// NewCircuitBreaker creates a circuit breaker.
//   - maxFailures: consecutive failures before opening the circuit.
//   - timeout: how long the circuit stays open before moving to half-open.
func NewCircuitBreaker(maxFailures int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		timeout:     timeout,
		state:       StateClosed,
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.currentState()
}

// currentState returns the state, transitioning from open to half-open if the
// timeout has elapsed. Must be called with cb.mu held.
func (cb *CircuitBreaker) currentState() CircuitState {
	if cb.state == StateOpen && time.Since(cb.lastFailure) > cb.timeout {
		cb.state = StateHalfOpen
		slog.Info("circuit breaker transitioning", "to", StateHalfOpen)
	}
	return cb.state
}

// recordSuccess resets the failure counter and closes the circuit.
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	if cb.state == StateHalfOpen {
		slog.Info("circuit breaker transitioning", "to", StateClosed)
	}
	cb.state = StateClosed
}

// recordFailure increments the failure counter and potentially opens the circuit.
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.lastFailure = time.Now()
	if cb.failures >= cb.maxFailures {
		if cb.state != StateOpen {
			slog.Warn("circuit breaker opening", "failures", cb.failures)
		}
		cb.state = StateOpen
	}
}

// responseRecorder captures the status code from downstream handlers.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

// Wrap returns a middleware that enforces the circuit breaker.
func (cb *CircuitBreaker) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := cb.State()

		if state == StateOpen {
			http.Error(w, "service unavailable (circuit open)", http.StatusServiceUnavailable)
			return
		}

		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		if rec.statusCode >= 500 {
			cb.recordFailure()
		} else {
			cb.recordSuccess()
		}
	})
}
