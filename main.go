package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SafiullahRattar/gateway/internal/config"
	"github.com/SafiullahRattar/gateway/internal/proxy"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	// Structured JSON logging for production.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	gw, err := proxy.New(cfg)
	if err != nil {
		slog.Error("failed to create gateway", "err", err)
		os.Exit(1)
	}

	// Watch for config changes and hot-reload.
	watcher, err := config.NewWatcher(*configPath, func(newCfg *config.Config) {
		if err := gw.Reload(newCfg); err != nil {
			slog.Error("config reload failed", "err", err)
			return
		}
		slog.Info("gateway routes reloaded")
	})
	if err != nil {
		slog.Warn("config watcher disabled", "err", err)
	} else {
		defer watcher.Close()
	}

	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      gw,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	errCh := make(chan error, 1)
	go func() {
		slog.Info("gateway starting", "addr", cfg.Server.Addr)
		if cfg.Server.TLS != nil {
			errCh <- srv.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("shutting down", "signal", sig)
	case err := <-errCh:
		slog.Error("server error", "err", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gw.Stop()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
		os.Exit(1)
	}
	slog.Info("gateway stopped")
}
