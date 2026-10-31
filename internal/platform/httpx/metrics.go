package httpx

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics = http request metrics. label = mux pattern, never raw path (cardinality).
type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewMetrics registers collectors on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "crawler_http_requests_total", Help: "HTTP requests by route pattern and status.",
		}, []string{"pattern", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "crawler_http_request_duration_seconds", Help: "HTTP latency by route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"pattern"}),
	}
	reg.MustRegister(m.requests, m.duration)
	return m
}

// Middleware must wrap mux directly: mux sets r.Pattern on same request.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		p := r.Pattern
		if p == "" {
			p = "unmatched"
		}
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		m.requests.WithLabelValues(p, strconv.Itoa(rec.status)).Inc()
		m.duration.WithLabelValues(p).Observe(time.Since(start).Seconds())
	})
}
