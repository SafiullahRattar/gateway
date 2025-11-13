package health

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/SafiullahRattar/gateway/internal/balancer"
)

// Checker periodically probes backends and marks them healthy or unhealthy.
type Checker struct {
	backends []*balancer.Backend
	path     string
	interval time.Duration
	timeout  time.Duration
	client   *http.Client

	mu      sync.Mutex
	cancel  context.CancelFunc
	stopped bool
}

// NewChecker creates a health checker.
func NewChecker(backends []*balancer.Backend, path string, interval, timeout time.Duration) *Checker {
	return &Checker{
		backends: backends,
		path:     path,
		interval: interval,
		timeout:  timeout,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Start begins periodic health checking in the background.
func (c *Checker) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()

	go c.loop(ctx)
}

// Stop halts the health checker.
func (c *Checker) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	c.stopped = true
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Checker) loop(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Run an immediate check, then tick.
	c.checkAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkAll(ctx)
		}
	}
}

func (c *Checker) checkAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, b := range c.backends {
		wg.Add(1)
		go func(backend *balancer.Backend) {
			defer wg.Done()
			c.check(ctx, backend)
		}(b)
	}
	wg.Wait()
}

func (c *Checker) check(ctx context.Context, b *balancer.Backend) {
	url := b.URL.String() + c.path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		b.SetHealthy(false)
		return
	}

	resp, err := c.client.Do(req)
	if err != nil {
		if b.IsHealthy() {
			slog.Warn("backend unhealthy", "url", b.URL, "err", err)
		}
		b.SetHealthy(false)
		return
	}
	resp.Body.Close()

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 400
	if healthy && !b.IsHealthy() {
		slog.Info("backend recovered", "url", b.URL)
	} else if !healthy && b.IsHealthy() {
		slog.Warn("backend unhealthy", "url", b.URL, "status", resp.StatusCode)
	}
	b.SetHealthy(healthy)
}

// CheckOnce probes a single backend and returns its health status.
// Useful for testing.
func (c *Checker) CheckOnce(ctx context.Context, b *balancer.Backend) bool {
	c.check(ctx, b)
	return b.IsHealthy()
}
