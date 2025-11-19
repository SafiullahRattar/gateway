# Gateway

A high-performance API gateway and reverse proxy built in Go, designed for microservices architectures.

## The Problem

As microservices grow, every client needs to know the address of every service, handle retries, manage authentication, and deal with cross-cutting concerns like rate limiting and logging. An API gateway sits between clients and backend services, providing a single entry point that handles routing, load balancing, fault tolerance, and observability -- so individual services don't have to.

## Architecture

```
                         ┌─────────────────────────────────────────────────┐
                         │                   Gateway                       │
                         │                                                 │
  ┌─────────┐           │  ┌─────────┐  ┌──────────┐  ┌──────────────┐  │   ┌───────────┐
  │         │  request   │  │         │  │          │  │              │  │   │ Backend 1 │
  │         │──────────▶ │  │ Metrics │─▶│ Logging  │─▶│ Rate Limiter │──│──▶│ (healthy) │
  │         │           │  │         │  │          │  │              │  │   └───────────┘
  │ Client  │           │  └─────────┘  └──────────┘  └──────┬───────┘  │
  │         │           │                                     │          │   ┌───────────┐
  │         │◀──────────│──────────────────────────────┐      ▼          │   │ Backend 2 │
  │         │  response │                              │  ┌──────────┐  │──▶│ (healthy) │
  └─────────┘           │                              │  │ Circuit  │  │   └───────────┘
                         │                              │  │ Breaker  │  │
                         │                              │  └────┬─────┘  │   ┌───────────┐
  ┌─────────┐           │  ┌───────────────────────┐   │       │         │   │ Backend 3 │
  │  Prom   │◀──────────│──│ /metrics endpoint     │   │       ▼         │──▶│ (down)    │
  └─────────┘           │  └───────────────────────┘   │  ┌──────────┐  │   └───────────┘
                         │                              │  │   Auth   │  │
                         │  ┌───────────────────────┐   │  │  (JWT)   │  │
                         │  │ Health Checker         │   │  └────┬─────┘  │
                         │  │ (probes backends)      │   │       │         │
                         │  └───────────────────────┘   │       ▼         │
                         │                              │  ┌──────────┐  │
                         │                              │  │   CORS   │  │
                         │                              │  └────┬─────┘  │
                         │                              │       │         │
                         │                              │       ▼         │
                         │                              │  ┌──────────┐  │
                         │                              └──│  Proxy   │  │
                         │                                 │ (select  │  │
                         │                                 │ backend) │  │
                         │                                 └──────────┘  │
                         └─────────────────────────────────────────────────┘
```

Request flow: **Client -> Metrics -> Logging -> Rate Limiter -> Circuit Breaker -> Auth -> CORS -> Reverse Proxy -> Backend**

## Features

### Reverse Proxy
Routes requests to backend services based on URL path and host matching. Built on Go's `net/http/httputil.ReverseProxy` for production-grade proxying with connection pooling and HTTP/2 support.

### Load Balancing
Three strategies to distribute traffic across backends:
- **Round Robin** -- even distribution across all healthy backends
- **Least Connections** -- routes to the backend with the fewest active requests
- **Weighted Round Robin** -- proportional distribution based on configured weights

### Rate Limiting
Token-bucket algorithm with per-client tracking (by IP address). Each route can define its own rate and burst limits, with a global default fallback. Clients exceeding their limit receive `429 Too Many Requests`.

### Circuit Breaker
Protects backends from cascading failures using a three-state model:
- **Closed** -- normal operation, tracking consecutive failures
- **Open** -- after N failures, all requests are rejected with `503`
- **Half-Open** -- after a timeout, one request is allowed through to test recovery

### JWT Authentication
Optional per-route JWT validation using HMAC signing. Requests without a valid `Authorization: Bearer <token>` header are rejected with `401`.

### CORS
Configurable Cross-Origin Resource Sharing headers with preflight (`OPTIONS`) handling. Supports per-route origin whitelisting.

### Health Checks
Active health probing runs in the background, periodically hitting each backend's health endpoint. Unhealthy backends are automatically removed from the load balancer pool and re-added when they recover.

### Prometheus Metrics
Exposes a `/metrics` endpoint with:
- `gateway_requests_total` -- request count by method, path, and status code
- `gateway_request_duration_seconds` -- latency histogram
- `gateway_backend_health` -- backend health gauge (1=up, 0=down)
- `gateway_active_connections` -- current in-flight requests
- `gateway_errors_total` -- proxy error count

### Hot Reload
The gateway watches `config.yaml` for changes using `fsnotify`. When the file is modified, routes and backends are rebuilt without restarting the process. In-flight requests complete against the old configuration.

### Graceful Shutdown
On `SIGINT` or `SIGTERM`, the gateway stops accepting new connections, waits for in-flight requests to complete (up to 30 seconds), and then exits cleanly.

## Configuration Reference

```yaml
server:
  addr: ":8080"              # Listen address
  read_timeout: 15s          # Max time to read request headers
  write_timeout: 15s         # Max time to write response
  idle_timeout: 60s          # Max time for idle keep-alive connections
  tls:                       # Optional: enable HTTPS / HTTP/2
    cert_file: /path/to/cert.pem
    key_file: /path/to/key.pem

defaults:
  health_check:
    interval: 10s            # Time between health probes
    timeout: 2s              # Max wait for health response
    path: /health            # Backend health endpoint path
  circuit_breaker:
    max_failures: 5          # Consecutive failures before opening
    timeout: 30s             # Time in open state before half-open
  rate_limit:                # Global default (overridden per-route)
    rate: 100                # Tokens per second
    burst: 200               # Max burst size

routes:
  - path: /api/users         # URL prefix to match
    host: api.example.com    # Optional: match by Host header
    strip_path: true         # Remove matched prefix before forwarding
    balancer: round-robin    # round-robin | least-conn | weighted
    backends:
      - url: http://host:port
        weight: 1            # Relative weight (for weighted balancer)
    rate_limit:              # Optional: per-route rate limit
      rate: 50
      burst: 100
    auth:                    # Optional: require JWT
      jwt_secret: my-secret
    cors:                    # Optional: CORS configuration
      allow_origins:
        - "https://example.com"
      allow_methods: [GET, POST]
      allow_headers: [Content-Type, Authorization]
      max_age: 86400
```

## Quick Start

### With Docker

```bash
# Build the image
docker build -t gateway .

# Run with your config
docker run -p 8080:8080 -v $(pwd)/config.yaml:/etc/gateway/config.yaml gateway
```

### From Source

```bash
# Build
go build -o gateway .

# Run
./gateway -config config.yaml
```

## Usage Examples

### Basic Proxy Setup

Route all `/api/users` traffic to two backend instances with round-robin balancing:

```yaml
routes:
  - path: /api/users
    balancer: round-robin
    backends:
      - url: http://users-svc-1:8080
      - url: http://users-svc-2:8080
```

### Weighted Traffic Splitting

Send 80% of traffic to the primary and 20% to a canary deployment:

```yaml
routes:
  - path: /api/products
    balancer: weighted
    backends:
      - url: http://products-stable:8080
        weight: 4
      - url: http://products-canary:8080
        weight: 1
```

### Protected Route with Rate Limiting

Require JWT authentication and limit to 30 requests/second per client:

```yaml
routes:
  - path: /api/admin
    backends:
      - url: http://admin-svc:8080
    auth:
      jwt_secret: your-secret
    rate_limit:
      rate: 30
      burst: 60
```

### Monitoring

Scrape metrics with Prometheus:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: gateway
    static_configs:
      - targets: ['gateway:8080']
    metrics_path: /metrics
```

## Performance

The gateway is built directly on Go's `net/http` server and `net/http/httputil.ReverseProxy`, which means:

- Connection pooling to backends is handled automatically
- HTTP/2 is supported when TLS is configured
- The proxy reuses allocations and buffers from the standard library
- Middleware is composed as handler wrappers with zero additional allocations on the hot path
- Per-client rate limiter state uses `sync.Map` for lock-free reads
- Backend health state uses `sync.RWMutex` for minimal contention

## Testing

```bash
# Unit tests
go test ./internal/...

# Integration tests
go test ./test/...

# All tests
go test ./...
```

## License

MIT
