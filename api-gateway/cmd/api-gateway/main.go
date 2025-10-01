package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/clients"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/config"
	gwhttp "github.com/pribylovaa/go-news-aggregator/api-gateway/internal/http"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.Parse()

	cfg := config.MustLoad(configPath)

	log := setupLogger(cfg.Env)
	slog.SetDefault(log)
	log.Info("starting api-gateway", "env", cfg.Env)

	rootCtx, rootCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer rootCancel()

	cl, err := clients.New(rootCtx, *cfg, log)
	if err != nil {
		log.Error("clients_init_failed", slog.String("err", err.Error()))
		os.Exit(1)
	}

	defer func() {
		if cerr := cl.Close(); cerr != nil {
			log.Warn("clients_close_failed", slog.String("err", cerr.Error()))
		}
	}()

	log.Info("clients_initialized")

	opts := gwhttp.Options{
		Logger:   log,
		Timeout:  cfg.Timeouts.Service,
		BasePath: "",
	}

	apiHandler := gwhttp.NewRouter(cl, opts)

	var ready int32 // 0 — not ready; 1 — ready

	mux := http.NewServeMux()
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if atomic.LoadInt32(&ready) == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}

		http.Error(w, "not ready", http.StatusServiceUnavailable)
	})

	mux.Handle("/metrics", promhttp.Handler())

	mux.Handle("", apiHandler)

	httpAddr := cfg.HTTP.Addr()
	httpSrv := &http.Server{
		Addr:              httpAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", httpAddr)
	if err != nil {
		log.Error("http_listen_failed", slog.String("addr", httpAddr), slog.String("err", err.Error()))
		os.Exit(1)
	}

	log.Info("http_listen_start", slog.String("addr", httpAddr))

	serveErrCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- err
		}
		close(serveErrCh)
	}()

	atomic.StoreInt32(&ready, 1)
	log.Info("gateway_ready")

	select {
	case <-rootCtx.Done():
		log.Info("shutdown_requested")
	case err := <-serveErrCh:
		if err != nil {
			log.Error("http_serve_failed", slog.String("err", err.Error()))
		}
	}

	atomic.StoreInt32(&ready, 0)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http_shutdown_incomplete", slog.String("err", err.Error()))
	} else {
		log.Info("http_stopped")
	}

	log.Info("service_stopped")
}

func setupLogger(env string) *slog.Logger {
	switch env {
	case envLocal:
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case envDev:
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case envProd:
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	default:
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
}
