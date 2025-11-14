package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// RequestsTotal counts all proxied requests by method, path, and status.
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "requests_total",
		Help:      "Total number of proxied HTTP requests.",
	}, []string{"method", "path", "status"})

	// RequestDuration records latency histograms by method and path.
	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gateway",
		Name:      "request_duration_seconds",
		Help:      "Request latency distribution in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path"})

	// BackendHealth tracks the health state of each backend (1=healthy, 0=unhealthy).
	BackendHealth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "backend_health",
		Help:      "Health status of upstream backends (1=healthy, 0=unhealthy).",
	}, []string{"backend"})

	// ActiveConnections tracks the number of currently active connections.
	ActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "gateway",
		Name:      "active_connections",
		Help:      "Number of currently active connections.",
	})

	// ErrorsTotal counts proxy errors.
	ErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gateway",
		Name:      "errors_total",
		Help:      "Total number of proxy errors by type.",
	}, []string{"type"})
)

// Handler returns the Prometheus metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// metricsWriter captures the status code for recording metrics.
type metricsWriter struct {
	http.ResponseWriter
	status int
}

func (mw *metricsWriter) WriteHeader(code int) {
	mw.status = code
	mw.ResponseWriter.WriteHeader(code)
}

// Instrument returns a middleware that records request metrics.
func Instrument(routePath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ActiveConnections.Inc()
			defer ActiveConnections.Dec()

			start := time.Now()
			mw := &metricsWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(mw, r)

			duration := time.Since(start).Seconds()
			status := strconv.Itoa(mw.status)

			RequestsTotal.WithLabelValues(r.Method, routePath, status).Inc()
			RequestDuration.WithLabelValues(r.Method, routePath).Observe(duration)
		})
	}
}
