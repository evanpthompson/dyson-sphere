// Package server holds the HTTP surface: routes, handlers, and the readiness
// state that Kubernetes probes read.
package server

import (
	"log/slog"
	"net/http"
	"sync/atomic"
)

// Server owns everything the HTTP handlers need. In Go you pass dependencies in
// explicitly rather than reaching for globals -- it is what makes the handlers
// testable without standing up the whole program.
type Server struct {
	log *slog.Logger

	// ready is flipped to false the moment shutdown begins, so Kubernetes stops
	// routing new traffic here while in-flight requests finish. atomic.Bool
	// because probes are served on other goroutines than the one shutting down.
	ready atomic.Bool
}

// New returns a Server that is alive but NOT yet ready. Call MarkReady once
// dependencies (databases, caches, queues) are actually usable.
func New(log *slog.Logger) *Server {
	return &Server{log: log}
}

func (s *Server) MarkReady()    { s.ready.Store(true) }
func (s *Server) MarkNotReady() { s.ready.Store(false) }

// Routes builds the HTTP handler. Go 1.22+ lets the standard mux match on
// method and path together, so no third-party router is needed here.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	return s.withLogging(mux)
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

func writePlain(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body + "\n"))
}

// statusRecorder wraps http.ResponseWriter to remember the status code, since
// the interface has no getter for it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// withLogging is middleware: a function taking a handler and returning a
// handler. Session 2 replaces the hand-rolled timing here with OTel.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
		)
	})
}
