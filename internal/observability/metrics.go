package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// RequestsTotal counts /process requests by direction and status.
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pii_requests_total",
		Help: "Total number of /process requests.",
	}, []string{"type", "status"})

	// RequestLatency observes /process latency in seconds.
	RequestLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "pii_latency_seconds",
		Help:    "Latency of /process requests.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"type"})

	// DetectedTotal counts detected PII types.
	DetectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pii_detected_total",
		Help: "Total number of detected PII spans by type.",
	}, []string{"type"})
)

// QueueDepth is the current total depth of the worker-pool queues.
var QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "pii_queue_depth",
	Help: "Current total depth of the worker-pool queues.",
})

// WorkersBusy is the number of workers currently processing a job.
var WorkersBusy = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "pii_workers_busy",
	Help: "Number of workers currently processing a job.",
})

// RedisUp is 1 when the Redis circuit breaker is closed, 0 when open.
var RedisUp = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "pii_redis_up",
	Help: "1 when Redis is reachable, 0 when the circuit breaker is open.",
})

// Handler returns the Prometheus metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}