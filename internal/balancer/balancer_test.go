package balancer

import (
	"net/url"
	"testing"
)

func makeBackends(n int) []*Backend {
	backends := make([]*Backend, n)
	for i := range backends {
		backends[i] = &Backend{
			URL:     &url.URL{Scheme: "http", Host: "localhost:" + string(rune('0'+i))},
			Weight:  1,
			healthy: true,
		}
	}
	return backends
}

func TestRoundRobinDistribution(t *testing.T) {
	backends := makeBackends(3)
	rr := NewRoundRobin()

	counts := make(map[*Backend]int)
	for i := 0; i < 300; i++ {
		b := rr.Next(backends)
		if b == nil {
			t.Fatal("expected a backend, got nil")
		}
		counts[b]++
	}
	for _, b := range backends {
		if counts[b] != 100 {
			t.Errorf("expected 100 requests, got %d", counts[b])
		}
	}
}

func TestRoundRobinSkipsUnhealthy(t *testing.T) {
	backends := makeBackends(3)
	backends[1].SetHealthy(false)
	rr := NewRoundRobin()

	for i := 0; i < 10; i++ {
		b := rr.Next(backends)
		if b == backends[1] {
			t.Fatal("unhealthy backend should not be selected")
		}
	}
}

func TestRoundRobinNoHealthy(t *testing.T) {
	backends := makeBackends(2)
	backends[0].SetHealthy(false)
	backends[1].SetHealthy(false)
	rr := NewRoundRobin()

	if b := rr.Next(backends); b != nil {
		t.Fatal("expected nil when no healthy backends")
	}
}

func TestLeastConnPicksMinimum(t *testing.T) {
	backends := makeBackends(3)
	// Give backends 0 and 2 some connections.
	backends[0].IncrConns()
	backends[0].IncrConns()
	backends[2].IncrConns()

	lc := NewLeastConn()
	b := lc.Next(backends)
	if b != backends[1] {
		t.Fatalf("expected backend[1] (0 conns), got %v with %d conns", b.URL, b.ActiveConns())
	}
}

func TestLeastConnSkipsUnhealthy(t *testing.T) {
	backends := makeBackends(2)
	backends[0].SetHealthy(false)
	lc := NewLeastConn()
	b := lc.Next(backends)
	if b != backends[1] {
		t.Fatal("expected healthy backend[1]")
	}
}

func TestWeightedDistribution(t *testing.T) {
	backends := makeBackends(2)
	backends[0].Weight = 3
	backends[1].Weight = 1

	w := NewWeightedRoundRobin()
	counts := make(map[*Backend]int)
	for i := 0; i < 400; i++ {
		b := w.Next(backends)
		if b == nil {
			t.Fatal("expected a backend")
		}
		counts[b]++
	}
	// With weights 3:1, expect ~75%:25%.
	ratio := float64(counts[backends[0]]) / float64(counts[backends[1]])
	if ratio < 2.5 || ratio > 3.5 {
		t.Errorf("expected ratio ~3, got %.2f (counts: %d, %d)", ratio, counts[backends[0]], counts[backends[1]])
	}
}

func TestNewStrategy(t *testing.T) {
	for _, name := range []string{"round-robin", "least-conn", "weighted", ""} {
		s, err := New(name)
		if err != nil {
			t.Errorf("New(%q) returned error: %v", name, err)
		}
		if s == nil {
			t.Errorf("New(%q) returned nil", name)
		}
	}
	_, err := New("unknown")
	if err == nil {
		t.Error("expected error for unknown strategy")
	}
}

func TestBackendConnTracking(t *testing.T) {
	b := &Backend{URL: &url.URL{}, healthy: true}
	if b.ActiveConns() != 0 {
		t.Fatal("expected 0 initial conns")
	}
	b.IncrConns()
	b.IncrConns()
	if b.ActiveConns() != 2 {
		t.Fatalf("expected 2 conns, got %d", b.ActiveConns())
	}
	b.DecrConns()
	if b.ActiveConns() != 1 {
		t.Fatalf("expected 1 conn, got %d", b.ActiveConns())
	}
	b.DecrConns()
	b.DecrConns() // should not go negative
	if b.ActiveConns() != 0 {
		t.Fatalf("expected 0 conns, got %d", b.ActiveConns())
	}
}
