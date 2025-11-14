package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"

	"github.com/SafiullahRattar/gateway/internal/balancer"
	"github.com/SafiullahRattar/gateway/internal/config"
	"github.com/SafiullahRattar/gateway/internal/health"
	"github.com/SafiullahRattar/gateway/internal/metrics"
	"github.com/SafiullahRattar/gateway/internal/middleware"
)

// Gateway is the main request router and reverse proxy.
type Gateway struct {
	mu       sync.RWMutex
	mux      *http.ServeMux
	cfg      *config.Config
	checkers []*health.Checker
}

// New creates a Gateway from the given configuration.
func New(cfg *config.Config) (*Gateway, error) {
	g := &Gateway{cfg: cfg}
	if err := g.buildRoutes(cfg); err != nil {
		return nil, err
	}
	return g, nil
}

// ServeHTTP dispatches to the internal mux.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.RLock()
	mux := g.mux
	g.mu.RUnlock()
	mux.ServeHTTP(w, r)
}

// Reload applies a new configuration, rebuilding routes and health checkers.
func (g *Gateway) Reload(cfg *config.Config) error {
	g.stopCheckers()
	if err := g.buildRoutes(cfg); err != nil {
		return err
	}
	g.mu.Lock()
	g.cfg = cfg
	g.mu.Unlock()
	return nil
}

// Stop shuts down health checkers.
func (g *Gateway) Stop() {
	g.stopCheckers()
}

func (g *Gateway) stopCheckers() {
	g.mu.Lock()
	checkers := g.checkers
	g.checkers = nil
	g.mu.Unlock()
	for _, c := range checkers {
		c.Stop()
	}
}

func (g *Gateway) buildRoutes(cfg *config.Config) error {
	mux := http.NewServeMux()
	var checkers []*health.Checker

	// Register the Prometheus metrics endpoint.
	mux.Handle("GET /metrics", metrics.Handler())

	for _, route := range cfg.Routes {
		backends, err := buildBackends(route.Backends)
		if err != nil {
			return err
		}

		strategy, err := balancer.New(route.Balancer)
		if err != nil {
			return err
		}

		// Set up the reverse proxy.
		rp := &httputil.ReverseProxy{
			Director: makeDirector(route, backends, strategy),
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				slog.Error("proxy error", "err", err, "path", r.URL.Path)
				metrics.ErrorsTotal.WithLabelValues("proxy").Inc()
				http.Error(w, "bad gateway", http.StatusBadGateway)
			},
		}

		// Wrap with connection tracking for least-conn.
		var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rp.ServeHTTP(w, r)
		})

		// Build the middleware chain for this route.
		var mws []middleware.Middleware

		// Metrics (outermost so it captures full latency).
		mws = append(mws, metrics.Instrument(route.Path))

		// Logging.
		mws = append(mws, middleware.Logging)

		// CORS.
		if route.CORS != nil {
			mws = append(mws, middleware.CORS(middleware.CORSOptions{
				AllowOrigins: route.CORS.AllowOrigins,
				AllowMethods: route.CORS.AllowMethods,
				AllowHeaders: route.CORS.AllowHeaders,
				MaxAge:       route.CORS.MaxAge,
			}))
		}

		// Rate limiting.
		rl := route.RateLimit
		if rl == nil {
			rl = cfg.Defaults.RateLimit
		}
		if rl != nil {
			limiter := middleware.NewRateLimiter(rl.Rate, rl.Burst)
			mws = append(mws, limiter.Wrap)
		}

		// Circuit breaker.
		cb := middleware.NewCircuitBreaker(
			cfg.Defaults.CircuitBreaker.MaxFailures,
			cfg.Defaults.CircuitBreaker.Timeout,
		)
		mws = append(mws, cb.Wrap)

		// Auth.
		if route.Auth != nil {
			auth := middleware.NewJWTAuth(route.Auth.JWTSecret)
			mws = append(mws, auth.Wrap)
		}

		handler = middleware.Chain(handler, mws...)

		// Register the route pattern. Trailing slash patterns match subtrees.
		pattern := route.Path
		if !strings.HasSuffix(pattern, "/") {
			pattern += "/"
		}
		mux.Handle(pattern, handler)

		// Start health checks for these backends.
		checker := health.NewChecker(
			backends,
			cfg.Defaults.HealthCheck.Path,
			cfg.Defaults.HealthCheck.Interval,
			cfg.Defaults.HealthCheck.Timeout,
		)
		checker.Start()
		checkers = append(checkers, checker)

		// Register health gauges.
		for _, b := range backends {
			metrics.BackendHealth.WithLabelValues(b.URL.String()).Set(1)
		}
	}

	g.mu.Lock()
	g.mux = mux
	g.checkers = checkers
	g.mu.Unlock()

	return nil
}

func buildBackends(cfgBackends []config.BackendConfig) ([]*balancer.Backend, error) {
	backends := make([]*balancer.Backend, 0, len(cfgBackends))
	for _, bc := range cfgBackends {
		b, err := balancer.NewBackend(bc.URL, bc.Weight)
		if err != nil {
			return nil, err
		}
		backends = append(backends, b)
	}
	return backends, nil
}

// makeDirector returns an httputil.ReverseProxy Director that selects a backend
// using the given strategy and rewrites the request.
func makeDirector(route config.Route, backends []*balancer.Backend, strategy balancer.Strategy) func(*http.Request) {
	return func(r *http.Request) {
		backend := strategy.Next(backends)
		if backend == nil {
			slog.Error("no healthy backends", "route", route.Path)
			return
		}

		// Track connections for least-conn balancing.
		backend.IncrConns()

		r.URL.Scheme = backend.URL.Scheme
		r.URL.Host = backend.URL.Host

		if route.StripPath {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, route.Path)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}

		// Preserve the original host or set the backend host.
		r.Host = backend.URL.Host
	}
}
