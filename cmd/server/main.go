// Command server runs the Roundly API and serves the embedded frontend.
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

	"github.com/edingc/roundly/internal/config"
	"github.com/edingc/roundly/internal/database"
	"github.com/edingc/roundly/internal/server"
	"github.com/edingc/roundly/web"
)

const shutdownTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logLevel := slog.LevelInfo
	if !cfg.IsProd() {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))

	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	slog.Info("database ready", "path", cfg.DatabaseURL)

	frontend, err := web.Dist()
	if err != nil {
		// A missing frontend build is not fatal: the API is still fully usable,
		// which is what the Vite dev server relies on.
		slog.Warn("frontend assets unavailable, serving API only", "error", err)
		frontend = nil
	}

	var frontendHandler http.Handler
	if frontend != nil {
		frontendHandler = server.SPAHandler(frontend)
	}

	// Closed on shutdown so the server's background goroutines get a chance to
	// flush what they are holding rather than being killed mid-window.
	stopBackground := make(chan struct{})
	defer close(stopBackground)

	handler := server.New(cfg, db, frontendHandler, stopBackground)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	// Signal handling has to be installed before the listener starts so a fast
	// Ctrl-C is not missed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("listening",
			"addr", cfg.Addr,
			"env", cfg.Env,
			"google_oauth", cfg.GoogleEnabled(),
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	slog.Info("stopped")
	return nil
}
