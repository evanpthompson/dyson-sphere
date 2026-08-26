// Package server holds the HTTP surface: routes, handlers, and the readiness
// state that Kubernetes probes read.
package server

import (
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"gitlab.com/navetoocool/dyson-sphere/internal/observability"
)

// Server owns everything the HTTP handlers need. Dependencies are passed in
// explicitly rather than reached for globally -- that is what makes the
// handlers testable without standing up the whole program.
type Server struct {
	log     *slog.Logger
	metrics *observability.Metrics

	// ready flips to false the moment shutdown begins, so Kubernetes stops
	// routing new traffic here while in-flight requests finish. atomic.Bool
	// because probes are served on other goroutines than the one shutting down.
	ready atomic.Bool
}

// New returns a Server that is alive but NOT yet ready. Call MarkReady once
// dependencies (databases, caches, queues) are actually usable.
func New(log *slog.Logger, metrics *observability.Metrics) *Server {
	return &Server{log: log, metrics: metrics}
}

func (s *Server) MarkReady()    { s.ready.Store(true) }
func (s *Server) MarkNotReady() { s.ready.Store(false) }

// Routes builds the HTTP handler. Go 1.22+ lets the standard mux match on
// method and path together, so no third-party router is needed.
//
// Note what is deliberately NOT instrumented: probes and /metrics. Kubernetes
// hits /healthz and /readyz every few seconds forever, and Prometheus scrapes
// /metrics on the same cadence. Tracing them buries real traffic in noise and
// counting them makes every RED metric a measure of the monitoring system
// rather than the service.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.Handle("GET /metrics", s.metrics.Handler())

	mux.Handle("GET /api/hello", s.instrument("/api/hello", http.HandlerFunc(s.handleHello)))

	return mux
}

// instrument is the standard wrapper every business route gets: a trace span,
// RED metrics, and a log line carrying the trace ID. Applied per route rather
// than to the whole mux so the probe endpoints above can opt out.
func (s *Server) instrument(route string, h http.Handler) http.Handler {
	h = observability.Logging(s.log, route, h)
	h = s.metrics.Middleware(route, h)
	// otelhttp is outermost so the span exists before anything else runs, which
	// is what lets the log middleware read a trace ID off the request context.
	return otelhttp.NewHandler(h, route)
}

// handleHealthz answers liveness: is this process running at all? It must stay
// dumb. If it checks a database, one slow query gets your pod killed.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writePlain(w, http.StatusOK, "ok")
}

// handleReadyz answers readiness: should traffic be sent here right now?
// This is the one allowed to say no.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if !s.ready.Load() {
		writePlain(w, http.StatusServiceUnavailable, "not ready")
		return
	}
	writePlain(w, http.StatusOK, "ready")
}

// handleHello exists so there is real traffic to trace and measure. It creates
// a child span to show nesting -- in a real service this is where the database
// call or downstream request would sit.
func (s *Server) handleHello(w http.ResponseWriter, r *http.Request) {
	ctx, span := otel.Tracer("dyson-sphere/server").Start(r.Context(), "greet")
	defer span.End()

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "world"
	}
	// Attributes go on the span, not in the metric labels. Spans are sampled
	// and can carry unbounded values; metrics cannot.
	span.SetAttributes(attribute.String("greeting.name", name))

	// Stand-in for work, so the duration histogram has something to record.
	select {
	case <-time.After(3 * time.Millisecond):
	case <-ctx.Done():
	}

	writePlain(w, http.StatusOK, "hello, "+name)
}

func writePlain(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body + "\n"))
}
