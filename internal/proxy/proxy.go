package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/SafiullahRattar/gateway/internal/config"
)

// Gateway is the main request router and reverse proxy.
type Gateway struct {
	mu  sync.RWMutex
	mux *http.ServeMux
	cfg *config.Config
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

// Reload applies a new configuration, rebuilding routes.
func (g *Gateway) Reload(cfg *config.Config) error {
	if err := g.buildRoutes(cfg); err != nil {
		return err
	}
	g.mu.Lock()
	g.cfg = cfg
	g.mu.Unlock()
	return nil
}

// Stop is a no-op for now.
func (g *Gateway) Stop() {}

func (g *Gateway) buildRoutes(cfg *config.Config) error {
	mux := http.NewServeMux()

	for _, route := range cfg.Routes {
		if len(route.Backends) == 0 {
			continue
		}

		target, err := url.Parse(route.Backends[0].URL)
		if err != nil {
			return err
		}

		rp := &httputil.ReverseProxy{
			Director: makeDirector(route, target),
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				slog.Error("proxy error", "err", err, "path", r.URL.Path)
				http.Error(w, "bad gateway", http.StatusBadGateway)
			},
		}

		pattern := route.Path
		if !strings.HasSuffix(pattern, "/") {
			pattern += "/"
		}
		mux.Handle(pattern, rp)
	}

	g.mu.Lock()
	g.mux = mux
	g.mu.Unlock()

	return nil
}

func makeDirector(route config.Route, target *url.URL) func(*http.Request) {
	return func(r *http.Request) {
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host

		if route.StripPath {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, route.Path)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}

		r.Host = target.Host
	}
}
