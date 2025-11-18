package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SafiullahRattar/gateway/internal/balancer"
)

func TestCheckerMarksHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	b, err := balancer.NewBackend(srv.URL, 1)
	if err != nil {
		t.Fatal(err)
	}

	c := NewChecker([]*balancer.Backend{b}, "/health", 1*time.Second, 1*time.Second)
	ok := c.CheckOnce(context.Background(), b)
	if !ok {
		t.Fatal("expected backend to be healthy")
	}
}

func TestCheckerMarksUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b, err := balancer.NewBackend(srv.URL, 1)
	if err != nil {
		t.Fatal(err)
	}

	c := NewChecker([]*balancer.Backend{b}, "/health", 1*time.Second, 1*time.Second)
	ok := c.CheckOnce(context.Background(), b)
	if ok {
		t.Fatal("expected backend to be unhealthy")
	}
}

func TestCheckerHandlesDownBackend(t *testing.T) {
	b, err := balancer.NewBackend("http://127.0.0.1:1", 1) // nothing listening
	if err != nil {
		t.Fatal(err)
	}

	c := NewChecker([]*balancer.Backend{b}, "/health", 1*time.Second, 500*time.Millisecond)
	ok := c.CheckOnce(context.Background(), b)
	if ok {
		t.Fatal("expected unreachable backend to be unhealthy")
	}
}

func TestCheckerStartStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	b, err := balancer.NewBackend(srv.URL, 1)
	if err != nil {
		t.Fatal(err)
	}

	c := NewChecker([]*balancer.Backend{b}, "/health", 50*time.Millisecond, 1*time.Second)
	c.Start()
	time.Sleep(200 * time.Millisecond)
	c.Stop()
	// Should not panic on double stop.
	c.Stop()

	if !b.IsHealthy() {
		t.Fatal("expected backend to be healthy after checker ran")
	}
}
