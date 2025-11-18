package test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/SafiullahRattar/gateway/internal/config"
	"github.com/SafiullahRattar/gateway/internal/proxy"
)

func integrationConfig(urls ...string) *config.Config {
	backends := make([]config.BackendConfig, len(urls))
	for i, u := range urls {
		backends[i] = config.BackendConfig{URL: u, Weight: 1}
	}
	return &config.Config{
		Server: config.ServerConfig{Addr: ":0"},
		Routes: []config.Route{
			{
				Path:     "/api",
				Balancer: "round-robin",
				Backends: backends,
				RateLimit: &config.RateLimitRule{
					Rate:  1000,
					Burst: 100,
				},
			},
		},
		Defaults: config.DefaultsConfig{
			HealthCheck: config.HealthCheckConfig{
				Interval: 1 * time.Hour, // effectively disabled for tests
				Timeout:  1 * time.Second,
				Path:     "/health",
			},
			CircuitBreaker: config.CircuitBreakerConfig{
				MaxFailures: 5,
				Timeout:     30 * time.Second,
			},
		},
	}
}

func TestIntegrationRoundRobinDistribution(t *testing.T) {
	var mu sync.Mutex
	counts := make(map[string]int)

	makeServer := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ignore health check requests from the background checker.
			if r.URL.Path != "/health" {
				mu.Lock()
				counts[name]++
				mu.Unlock()
			}
			w.Write([]byte(name))
		}))
	}

	s1 := makeServer("s1")
	s2 := makeServer("s2")
	s3 := makeServer("s3")
	defer s1.Close()
	defer s2.Close()
	defer s3.Close()

	cfg := integrationConfig(s1.URL, s2.URL, s3.URL)
	gw, err := proxy.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	ts := httptest.NewServer(gw)
	defer ts.Close()

	client := ts.Client()
	for i := 0; i < 30; i++ {
		resp, err := client.Get(ts.URL + "/api/test")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"s1", "s2", "s3"} {
		if counts[name] != 10 {
			t.Errorf("expected 10 requests to %s, got %d", name, counts[name])
		}
	}
}

func TestIntegrationConcurrentRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	cfg := integrationConfig(upstream.URL)
	cfg.Routes[0].RateLimit = &config.RateLimitRule{Rate: 10000, Burst: 10000}
	gw, err := proxy.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	ts := httptest.NewServer(gw)
	defer ts.Close()

	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := ts.Client()
			resp, err := client.Get(ts.URL + "/api/concurrent")
			if err != nil {
				errors <- err
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if string(body) != "ok" {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent request error: %v", err)
		}
	}
}

func TestIntegrationRateLimiting(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := integrationConfig(upstream.URL)
	cfg.Routes[0].RateLimit = &config.RateLimitRule{Rate: 5, Burst: 3}
	gw, err := proxy.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	ts := httptest.NewServer(gw)
	defer ts.Close()

	client := ts.Client()
	limited := 0
	for i := 0; i < 10; i++ {
		resp, err := client.Get(ts.URL + "/api/limited")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			limited++
		}
	}

	if limited == 0 {
		t.Fatal("expected some requests to be rate limited")
	}
}

func TestIntegrationCircuitBreaker(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	cfg := integrationConfig(upstream.URL)
	cfg.Defaults.CircuitBreaker.MaxFailures = 3
	cfg.Defaults.CircuitBreaker.Timeout = 5 * time.Second
	gw, err := proxy.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	ts := httptest.NewServer(gw)
	defer ts.Close()

	client := ts.Client()

	// Trigger failures to open the circuit.
	for i := 0; i < 3; i++ {
		resp, err := client.Get(ts.URL + "/api/failing")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	// Next request should be rejected by the circuit breaker.
	resp, err := client.Get(ts.URL + "/api/failing")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (circuit open), got %d", resp.StatusCode)
	}
}
