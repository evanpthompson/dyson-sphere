// Package observability wires the two signals every service on the paved road
// gets for free: distributed traces and Prometheus metrics.
//
// Design note for anyone reading this later: traces go out over OTLP to Tempo,
// but metrics are exposed in Prometheus text format for kube-prometheus-stack
// to scrape rather than pushed through OTel. Both are already running in the
// cluster, and using each one the way it expects to be used means one less
// translation layer to debug at 2am.
package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"gitlab.com/navetoocool/dyson-sphere/internal/build"
)

// ShutdownFunc flushes anything buffered and releases resources. Constructors
// that start background work return one of these instead of leaving the caller
// to guess what needs cleaning up. Call it on the way out or you will lose the
// spans still sitting in the batch processor.
type ShutdownFunc func(context.Context) error

// InitTracing configures the global tracer provider and returns its shutdown.
//
// Endpoint and most other knobs come from the standard OTEL_* environment
// variables, which the SDK reads on its own -- OTEL_EXPORTER_OTLP_ENDPOINT,
// OTEL_EXPORTER_OTLP_HEADERS, OTEL_TRACES_SAMPLER and so on. Not reinventing
// that configuration surface is the whole point of using the standard names.
func InitTracing(ctx context.Context) (ShutdownFunc, error) {
	// OTLP over HTTP rather than gRPC: fewer moving parts, passes through
	// ordinary proxies and ingress, and Tempo accepts both. Revisit if span
	// volume ever makes the encoding overhead matter.
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	// Resource attributes describe the emitter, not the request. They are what
	// lets you ask Tempo "show me every slow span from version 1.4.2".
	res, err := newResource()
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		// Batching, not one HTTP call per span.
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		// ParentBased(AlwaysSample) means: honour an upstream service's
		// sampling decision, and start sampling if we are the first hop.
		// Tail-based sampling is a platform concern and lands in Phase 3.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)

	otel.SetTracerProvider(tp)

	// Without a propagator, every service starts its own trace and you get a
	// pile of disconnected single-span traces instead of one request timeline.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func newResource() (*sdkResource, error) {
	return resourceFromAttrs(
		attribute.String("service.name", build.ServiceName),
		attribute.String("service.version", build.Version),
		attribute.String("service.commit", build.Commit),
	)
}
