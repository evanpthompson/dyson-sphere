// Command server is the reference service the dyson-sphere generator will emit.
// Build it by hand first; teach the generator to produce it in Session 5.
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

	"gitlab.com/navetoocool/dyson-sphere/internal/server"
)

const (
	defaultAddr     = ":8080"
	shutdownTimeout = 15 * time.Second
	readTimeout     = 5 * time.Second
	writeTimeout    = 10 * time.Second
	idleTimeout     = 60 * time.Second
)

func main() {
	// main() does nothing but call run() and translate an error into an exit
	// code. Keeping the logic in run() means it returns errors normally instead
	// of calling os.Exit, which skips deferred cleanup.
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

	// signal.NotifyContext gives a context cancelled on SIGINT/SIGTERM. This
	// replaces the old pattern of a signal channel plus a select loop.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := defaultAddr
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	app := server.New(log)

	// Always set timeouts explicitly. http.ListenAndServe has none, so one slow
	// client can hold a connection open indefinitely.
	srv := &http.Server{
		Addr:         addr,
		Handler:      app.Routes(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	// Serve on its own goroutine so main can wait on ctx. A buffered channel of
	// size 1 means this goroutine never blocks on send even if nobody reads.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", addr))
		// ErrServerClosed is what Shutdown causes. It is a normal stop, not a
		// failure, so it is filtered out here rather than at the read site.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	// Real services would announce readiness after dependencies connect.
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
