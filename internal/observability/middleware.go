package observability

import (
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

// statusRecorder wraps http.ResponseWriter to remember the status code, which
// the interface itself gives you no way to read back. One copy, used by both
// the logging and metrics middleware.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Logging emits one structured line per request, carrying the trace and span
// IDs so a log line can be pivoted straight to the trace that produced it.
// Correlating logs and traces after the fact is the difference between "the
// service was slow" and "this request was slow, here is where."
func Logging(log *slog.Logger, route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := []any{
			slog.String("route", route),
			slog.String("method", r.Method),
			slog.Int("status", rec.status),
		}
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			attrs = append(attrs,
				slog.String("trace_id", sc.TraceID().String()),
				slog.String("span_id", sc.SpanID().String()),
			)
		}
		log.LogAttrs(r.Context(), slog.LevelInfo, "request", toAttrs(attrs)...)
	})
}

func toAttrs(vals []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(vals))
	for _, v := range vals {
		if a, ok := v.(slog.Attr); ok {
			out = append(out, a)
		}
	}
	return out
}
