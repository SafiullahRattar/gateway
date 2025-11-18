package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SafiullahRattar/gateway/internal/config"
)

func testConfig(backendURL string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Addr: ":0"},
		Routes: []config.Route{
			{
				Path:     "/api",
				Balancer: "round-robin",
				Backends: []config.BackendConfig{
					{URL: backendURL, Weight: 1},
				},
			},
		},
		Defaults: config.DefaultsConfig{
			HealthCheck: config.HealthCheckConfig{
				Interval: 1e15, // very long so it doesn't fire during tests
				Timeout:  1e9,
				Path:     "/health",
			},
			CircuitBreaker: config.CircuitBreakerConfig{
				MaxFailures: 5,
				Timeout:     30e9,
			},
		},
	}
}

func TestProxyForwardsRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "test")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from backend"))
	}))
	defer upstream.Close()

	cfg := testConfig(upstream.URL)
	gw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Backend") != "test" {
		t.Fatal("expected X-Backend header from upstream")
	}
	if rec.Body.String() != "hello from backend" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestProxyStripPath(t *testing.T) {
	var receivedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore health check requests from the background checker.
		if r.URL.Path != "/health" {
			receivedPath = r.URL.Path
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := testConfig(upstream.URL)
	cfg.Routes[0].StripPath = true
	gw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/users/123", nil)
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedPath != "/users/123" {
		t.Fatalf("expected stripped path /users/123, got %s", receivedPath)
	}
}

func TestProxyReload(t *testing.T) {
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("v1"))
	}))
	defer upstream1.Close()

	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("v2"))
	}))
	defer upstream2.Close()

	cfg := testConfig(upstream1.URL)
	gw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	// Request goes to v1.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	gw.ServeHTTP(rec, req)
	if rec.Body.String() != "v1" {
		t.Fatalf("expected v1, got %s", rec.Body.String())
	}

	// Reload with new backend.
	cfg2 := testConfig(upstream2.URL)
	if err := gw.Reload(cfg2); err != nil {
		t.Fatal(err)
	}

	// Now request goes to v2.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/test", nil)
	gw.ServeHTTP(rec, req)
	if rec.Body.String() != "v2" {
		t.Fatalf("expected v2, got %s", rec.Body.String())
	}
}

func TestMetricsEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := testConfig(upstream.URL)
	gw, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Stop()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "" {
		t.Fatal("expected content-type header on /metrics")
	}
}
