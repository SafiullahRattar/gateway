package balancer

import "sync/atomic"

// RoundRobin distributes requests evenly across healthy backends.
type RoundRobin struct {
	counter atomic.Uint64
}

// NewRoundRobin creates a round-robin balancer.
func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

func (r *RoundRobin) Name() string { return "round-robin" }

func (r *RoundRobin) Next(backends []*Backend) *Backend {
	healthy := filterHealthy(backends)
	if len(healthy) == 0 {
		return nil
	}
	idx := r.counter.Add(1) - 1
	return healthy[idx%uint64(len(healthy))]
}

// WeightedRoundRobin distributes requests proportionally to backend weights.
type WeightedRoundRobin struct {
	counter atomic.Uint64
}

// NewWeightedRoundRobin creates a weighted round-robin balancer.
func NewWeightedRoundRobin() *WeightedRoundRobin {
	return &WeightedRoundRobin{}
}

func (w *WeightedRoundRobin) Name() string { return "weighted" }

func (w *WeightedRoundRobin) Next(backends []*Backend) *Backend {
	healthy := filterHealthy(backends)
	if len(healthy) == 0 {
		return nil
	}

	// Build a virtual list where each backend appears weight-many times.
	var total int
	for _, b := range healthy {
		total += b.Weight
	}
	idx := int(w.counter.Add(1)-1) % total
	for _, b := range healthy {
		idx -= b.Weight
		if idx < 0 {
			return b
		}
	}
	return healthy[len(healthy)-1]
}

func filterHealthy(backends []*Backend) []*Backend {
	out := make([]*Backend, 0, len(backends))
	for _, b := range backends {
		if b.IsHealthy() {
			out = append(out, b)
		}
	}
	return out
}
