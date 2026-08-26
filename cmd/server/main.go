// Command server is the reference service the dyson-sphere generator will emit.
// Built by hand first, on purpose: you cannot generate what you have not built.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gitlab.com/navetoocool/dyson-sphere/internal/build"
	"gitlab.com/navetoocool/dyson-sphere/internal/observability"
	"gitlab.com/navetoocool/dyson-sphere/internal/server"
)

const (
	defaultAddr     = ":8080"
	shutdownTimeout = 15 * time.Second
	readTimeout     = 5 * time.Second
	writeTimeout    = 10 * time.Second
	idleTimeout     = 60 * time.Second
	// Flushing traces gets its own budget. Spans buffered in the batch
	// processor are lost if the process exits before they are exported, and
	// losing the telemetry for the shutdown is losing it exactly when you
	// most want it.
	traceFlushTimeout = 5 * time.Second
)

func main() {
	// main() does nothing but call run() and turn an error into an exit code.
	// Keeping the logic in run() lets it return errors normally instead of
	// calling os.Exit, which skips every deferred cleanup.
	if err := run(); err != nil {
		slog.Error("fatal", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	// signal.NotifyContext gives a context cancelled on SIGINT/SIGTERM,
	// replacing the older signal-channel-and-select pattern.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting",
		slog.String("service", build.ServiceName),
		slog.String("version", build.Version),
		slog.String("commit", build.Commit),
	)

	// Tracing is initialised before anything can serve, so no request is ever
	// handled by a process that cannot report on it.
	shutdownTracing, err := observability.InitTracing(ctx)
	if err != nil {
		// A telemetry backend being unreachable must not stop the service from
		// serving traffic. Degrade by observing less, never by refusing to run.
		log.Warn("tracing disabled", slog.String("err", err.Error()))
		shutdownTracing = func(context.Context) error { return nil }
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), traceFlushTimeout)
		defer cancel()
		if err := shutdownTracing(flushCtx); err != nil {
			log.Warn("trace flush failed", slog.String("err", err.Error()))
		}
	}()

	metrics := observability.NewMetrics()

	addr := defaultAddr
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	app := server.New(log, metrics)

	// Always set timeouts explicitly. http.ListenAndServe has none, so one slow
	// client can hold a connection open indefinitely.
	srv := &http.Server{
		Addr:         addr,
		Handler:      app.Routes(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	// Serve on its own goroutine so main can wait on ctx. The channel is
	// buffered so this goroutine never blocks on send even if nobody reads.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", addr))
		// ErrServerClosed is what Shutdown causes. That is a normal stop, so it
		// is filtered here rather than at the read site.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	// A real service announces readiness once its dependencies connect.
	app.MarkReady()
	log.Info("ready")

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Fail readiness first. The load balancer stops sending new work while
	// in-flight requests are still allowed to finish.
	app.MarkNotReady()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	log.Info("stopped cleanly")
	return nil
}
