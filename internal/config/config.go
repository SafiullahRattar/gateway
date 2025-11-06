package config

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// Config represents the top-level gateway configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Routes   []Route        `yaml:"routes"`
	Defaults DefaultsConfig `yaml:"defaults"`
}

// ServerConfig holds the listener settings.
type ServerConfig struct {
	Addr         string        `yaml:"addr"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
	IdleTimeout  time.Duration `yaml:"idle_timeout"`
	TLS          *TLSConfig    `yaml:"tls,omitempty"`
}

// TLSConfig holds TLS certificate paths.
type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Route defines how incoming requests are matched and forwarded.
type Route struct {
	Path       string          `yaml:"path"`
	Host       string          `yaml:"host,omitempty"`
	StripPath  bool            `yaml:"strip_path"`
	Backends   []BackendConfig `yaml:"backends"`
	Balancer   string          `yaml:"balancer"`
	RateLimit  *RateLimitRule  `yaml:"rate_limit,omitempty"`
	Auth       *AuthRule       `yaml:"auth,omitempty"`
	CORS       *CORSRule       `yaml:"cors,omitempty"`
}

// BackendConfig describes a single upstream server.
type BackendConfig struct {
	URL    string `yaml:"url"`
	Weight int    `yaml:"weight,omitempty"`
}

// RateLimitRule configures the token-bucket rate limiter for a route.
type RateLimitRule struct {
	Rate  float64 `yaml:"rate"`
	Burst int     `yaml:"burst"`
}

// AuthRule configures JWT authentication for a route.
type AuthRule struct {
	JWTSecret string `yaml:"jwt_secret"`
}

// CORSRule configures CORS headers for a route.
type CORSRule struct {
	AllowOrigins []string `yaml:"allow_origins"`
	AllowMethods []string `yaml:"allow_methods"`
	AllowHeaders []string `yaml:"allow_headers"`
	MaxAge       int      `yaml:"max_age"`
}

// DefaultsConfig provides fallback values for routes that omit them.
type DefaultsConfig struct {
	HealthCheck    HealthCheckConfig    `yaml:"health_check"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"`
	RateLimit      *RateLimitRule       `yaml:"rate_limit,omitempty"`
}

// HealthCheckConfig controls active health probing.
type HealthCheckConfig struct {
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	Path     string        `yaml:"path"`
}

// CircuitBreakerConfig controls the circuit breaker behaviour.
type CircuitBreakerConfig struct {
	MaxFailures int           `yaml:"max_failures"`
	Timeout     time.Duration `yaml:"timeout"`
}

// Load reads and parses a YAML configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	setDefaults(cfg)
	return cfg, nil
}

func setDefaults(cfg *Config) {
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = 15 * time.Second
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = 15 * time.Second
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = 60 * time.Second
	}
	if cfg.Defaults.HealthCheck.Interval == 0 {
		cfg.Defaults.HealthCheck.Interval = 10 * time.Second
	}
	if cfg.Defaults.HealthCheck.Timeout == 0 {
		cfg.Defaults.HealthCheck.Timeout = 2 * time.Second
	}
	if cfg.Defaults.HealthCheck.Path == "" {
		cfg.Defaults.HealthCheck.Path = "/health"
	}
	if cfg.Defaults.CircuitBreaker.MaxFailures == 0 {
		cfg.Defaults.CircuitBreaker.MaxFailures = 5
	}
	if cfg.Defaults.CircuitBreaker.Timeout == 0 {
		cfg.Defaults.CircuitBreaker.Timeout = 30 * time.Second
	}
	for i := range cfg.Routes {
		if cfg.Routes[i].Balancer == "" {
			cfg.Routes[i].Balancer = "round-robin"
		}
		for j := range cfg.Routes[i].Backends {
			if cfg.Routes[i].Backends[j].Weight == 0 {
				cfg.Routes[i].Backends[j].Weight = 1
			}
		}
	}
}

// Watcher monitors a config file for changes and sends the new Config
// to subscribers through a channel.
type Watcher struct {
	path     string
	onChange func(*Config)
	watcher  *fsnotify.Watcher
	mu       sync.Mutex
	stopped  bool
}

// NewWatcher creates a file watcher for hot-reloading configuration.
func NewWatcher(path string, onChange func(*Config)) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}
	if err := fw.Add(path); err != nil {
		fw.Close()
		return nil, fmt.Errorf("watch path: %w", err)
	}
	w := &Watcher{
		path:     path,
		onChange: onChange,
		watcher:  fw,
	}
	go w.loop()
	return w, nil
}

func (w *Watcher) loop() {
	// debounce rapid writes
	var debounce <-chan time.Time
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				debounce = time.After(500 * time.Millisecond)
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("config watcher error", "err", err)
		case <-debounce:
			cfg, err := Load(w.path)
			if err != nil {
				slog.Error("reload config failed", "err", err)
				continue
			}
			slog.Info("config reloaded", "path", w.path)
			w.onChange(cfg)
		}
	}
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return nil
	}
	w.stopped = true
	return w.watcher.Close()
}
