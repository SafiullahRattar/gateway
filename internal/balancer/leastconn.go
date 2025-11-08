package balancer

// LeastConn picks the healthy backend with the fewest active connections.
type LeastConn struct{}

// NewLeastConn creates a least-connections balancer.
func NewLeastConn() *LeastConn {
	return &LeastConn{}
}

func (l *LeastConn) Name() string { return "least-conn" }

func (l *LeastConn) Next(backends []*Backend) *Backend {
	var best *Backend
	for _, b := range backends {
		if !b.IsHealthy() {
			continue
		}
		if best == nil || b.ActiveConns() < best.ActiveConns() {
			best = b
		}
	}
	return best
}
