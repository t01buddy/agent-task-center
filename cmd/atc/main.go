package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/t01buddy/agent-task-center/internal/api"
	"github.com/t01buddy/agent-task-center/internal/config"
	"github.com/t01buddy/agent-task-center/internal/dashboard"
	"github.com/t01buddy/agent-task-center/internal/db"
	"github.com/t01buddy/agent-task-center/internal/queue"
)

const version = "0.1.0"

func main() {
	versionFlag := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("atc version %s\n", version)
		os.Exit(0)
	}

	cfg := config.Load()

	var handler slog.Handler
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(os.Stdout, nil)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, nil)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	conn, err := db.OpenDefault(cfg.DBPath)
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.Handle("/", dashboard.MetricsPageHandler(conn))
	mux.Handle("/api/metrics-partial", dashboard.MetricsPartialHandler(conn))
	mux.Handle("/logs", dashboard.LogsHandler(conn))
	mux.Handle("/api/metrics", api.MetricsHandler(conn))

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	go func() {
		slog.Info("agent-task-center starting", "version", version, "addr", cfg.Addr, "db", cfg.DBPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	expiryCtx, expiryCancel := context.WithCancel(context.Background())
	defer expiryCancel()
	go queue.RunExpiryLoop(expiryCtx, conn, queue.ExpiryConfig{IntervalS: cfg.ExpiryIntervalS})

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down", "drain_timeout_s", cfg.DrainTimeoutS)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.DrainTimeoutS)*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
		os.Exit(1)
	}
	slog.Info("shutdown complete")
}
