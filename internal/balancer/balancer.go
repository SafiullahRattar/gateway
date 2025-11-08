package balancer

import (
	"fmt"
	"net/url"
	"sync"
)

// Backend represents an upstream server that can receive traffic.
type Backend struct {
	URL    *url.URL
	Weight int

	mu          sync.RWMutex
	healthy     bool
	activeConns int64
}

// NewBackend creates a backend from a raw URL and weight.
func NewBackend(rawURL string, weight int) (*Backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse backend url: %w", err)
	}
	if weight < 1 {
		weight = 1
	}
	return &Backend{
		URL:     u,
		Weight:  weight,
		healthy: true,
	}, nil
}

// IsHealthy reports whether the backend is considered healthy.
func (b *Backend) IsHealthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.healthy
}

// SetHealthy updates the health status.
func (b *Backend) SetHealthy(ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.healthy = ok
}

// ActiveConns returns the current number of active connections.
func (b *Backend) ActiveConns() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.activeConns
}

// IncrConns atomically increments the connection counter.
func (b *Backend) IncrConns() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.activeConns++
}

// DecrConns atomically decrements the connection counter.
func (b *Backend) DecrConns() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.activeConns > 0 {
		b.activeConns--
	}
}

// Strategy selects a healthy backend from the pool.
type Strategy interface {
	Next([]*Backend) *Backend
	Name() string
}

// New creates a Strategy by name.
func New(name string) (Strategy, error) {
	switch name {
	case "round-robin", "":
		return NewRoundRobin(), nil
	case "least-conn":
		return NewLeastConn(), nil
	case "weighted":
		return NewWeightedRoundRobin(), nil
	default:
		return nil, fmt.Errorf("unknown balancer strategy: %s", name)
	}
}
