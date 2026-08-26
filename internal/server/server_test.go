package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitlab.com/navetoocool/dyson-sphere/internal/observability"
)

func newTestServer(t *testing.T, ready bool) *Server {
	t.Helper()
	// A discard logger and a fresh private metrics registry per test. This is
	// why Metrics does not use the global Prometheus registerer -- with the
	// global one, the second test to run panics on duplicate registration.
	s := New(slog.New(slog.NewJSONHandler(io.Discard, nil)), observability.NewMetrics())
	if ready {
		s.MarkReady()
	}
	return s
}

// Table-driven tests are the Go idiom: one test function, a slice of cases.
func TestProbes(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		ready      bool
		wantStatus int
	}{
		{name: "healthz is ok before ready", path: "/healthz", ready: false, wantStatus: http.StatusOK},
		{name: "healthz is ok when ready", path: "/healthz", ready: true, wantStatus: http.StatusOK},
		{name: "readyz refuses before ready", path: "/readyz", ready: false, wantStatus: http.StatusServiceUnavailable},
		{name: "readyz accepts when ready", path: "/readyz", ready: true, wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, tt.ready)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GET %s: got status %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHello(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantBody string
	}{
		{name: "defaults to world", query: "", wantBody: "hello, world"},
		{name: "uses the name given", query: "?name=evan", wantBody: "hello, evan"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, true)

			req := httptest.NewRequest(http.MethodGet, "/api/hello"+tt.query, nil)
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("got status %d, want 200", rec.Code)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.wantBody {
				t.Errorf("got body %q, want %q", got, tt.wantBody)
			}
		})
	}
}

// TestMetricsRecorded proves the middleware is actually wired, not merely
// present. A metrics endpoint that returns 200 while recording nothing is the
// exact failure mode monitoring is supposed to prevent.
func TestMetricsRecorded(t *testing.T) {
	s := newTestServer(t, true)
	h := s.Routes()

	// Drive one instrumented request.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/hello", nil))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	body := rec.Body.String()
	for _, want := range []string{
		`http_requests_total{method="GET",route="/api/hello",status="200"} 1`,
		"http_request_duration_seconds_bucket",
		"http_requests_in_flight",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %q", want)
		}
	}
}

// TestProbesNotInstrumented locks in the deliberate choice from Routes():
// probe and scrape traffic must not pollute the RED metrics. Without this
// test, someone tidies the middleware later and every latency percentile
// silently becomes a measurement of kubelet.
func TestProbesNotInstrumented(t *testing.T) {
	s := newTestServer(t, true)
	h := s.Routes()

	for _, p := range []string{"/healthz", "/readyz"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if strings.Contains(rec.Body.String(), `route="/healthz"`) {
		t.Error("/healthz was recorded in RED metrics; probes must be excluded")
	}
	if strings.Contains(rec.Body.String(), `route="/readyz"`) {
		t.Error("/readyz was recorded in RED metrics; probes must be excluded")
	}
}
