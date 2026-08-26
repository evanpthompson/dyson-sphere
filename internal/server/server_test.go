package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Table-driven tests are the Go idiom: one test function, a slice of cases.
// Reviewers look for this specifically.
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
			s := New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
			if tt.ready {
				s.MarkReady()
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			s.Routes().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("GET %s: got status %d, want %d", tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}
