package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the RED signals -- Rate, Errors, Duration -- which are the
// three questions you actually ask of a request-serving service.
//
// It carries its own registry rather than using prometheus.DefaultRegisterer.
// A private registry means tests can build a Metrics, assert on it, and throw
// it away; with the global one, the second test panics on duplicate
// registration and you spend an afternoon finding out why.
type Metrics struct {
	registry *prometheus.Registry

	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

// NewMetrics registers the collectors and returns them ready to use.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,
		requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total HTTP requests, by route, method and status class.",
			},
			// Label on the ROUTE PATTERN ("/api/hello"), never the raw path.
			// Labelling on raw paths is the classic cardinality explosion: one
			// time series per unique URL, and Prometheus falls over.
			[]string{"route", "method", "status"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "http_request_duration_seconds",
				Help: "HTTP request latency in seconds.",
				// Buckets shape what questions you can answer later. These span
				// 5ms to 10s because that is the range this service should live
				// in; a p99 past the last bucket reads as +Inf and tells you
				// nothing useful.
				Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"route", "method"},
		),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
	}

	reg.MustRegister(m.requests, m.duration, m.inFlight)
	// Go runtime and process collectors: goroutines, GC pauses, heap, fds.
	// Free, and the first thing you want when a pod starts misbehaving.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// Handler serves the Prometheus text exposition format at /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Middleware records RED metrics for one route. It takes the route pattern
// explicitly rather than reading r.URL.Path, so cardinality stays bounded no
// matter what a caller puts in the URL.
func (m *Metrics) Middleware(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inFlight.Inc()
		defer m.inFlight.Dec()

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		m.duration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
		m.requests.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
	})
}
