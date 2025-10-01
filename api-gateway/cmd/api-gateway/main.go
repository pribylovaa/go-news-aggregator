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
		return
	}

	defer func() {
		if cerr := cl.Close(); cerr != nil {
			log.Warn("clients_close_failed", slog.String("err", cerr.Error()))
		}
	}()

	log.Info("clients_initialized")

	opts := gwhttp.Options{
		Logger:   slog.Default(),
		Timeout:  cfg.Timeouts.Service,
		BasePath: "/api",
	}

	apiHandler := gwhttp.NewRouter(cl, opts)

	var ready int32 // 0 — not ready; 1 — ready

	metricsMux := http.NewServeMux()
	metricsMux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	metricsMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if atomic.LoadInt32(&ready) == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}

		http.Error(w, "not ready", http.StatusServiceUnavailable)
	})

	metricsMux.Handle("/metrics", promhttp.Handler())

	metricsAddr := cfg.Metrics.Addr()
	metricsSrv := &http.Server{
		Addr:              metricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	apiMux := http.NewServeMux()
	apiMux.Handle("/", apiHandler)

	apiAddr := cfg.HTTP.Addr()
	apiSrv := &http.Server{
		Addr:              apiAddr,
		Handler:           apiMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	metricsLn, err := net.Listen("tcp", metricsAddr)
	if err != nil {
		log.Error("metrics_listen_failed", slog.String("addr", metricsAddr), slog.String("err", err.Error()))
		return
	}
	log.Info("metrics_listen_start", slog.String("addr", metricsAddr))

	apiLn, err := net.Listen("tcp", apiAddr)
	if err != nil {
		log.Error("http_listen_failed", slog.String("addr", apiAddr), slog.String("err", err.Error()))
		return
	}
	log.Info("http_listen_start", slog.String("addr", apiAddr))

	apiErrCh := make(chan error, 1)
	metricsErrCh := make(chan error, 1)

	go func() {
		if err := metricsSrv.Serve(metricsLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			metricsErrCh <- err
		}
		close(metricsErrCh)
	}()

	go func() {
		if err := apiSrv.Serve(apiLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			apiErrCh <- err
		}
		close(apiErrCh)
	}()

	atomic.StoreInt32(&ready, 1)
	log.Info("gateway_ready")

	select {
	case <-rootCtx.Done():
		log.Info("shutdown_requested")
	case err := <-metricsErrCh:
		if err != nil {
			log.Error("metrics_serve_failed", slog.String("err", err.Error()))
		}
	case err := <-apiErrCh:
		if err != nil {
			log.Error("http_serve_failed", slog.String("err", err.Error()))
		}
	}

	atomic.StoreInt32(&ready, 0)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		log.Warn("metrics_shutdown_incomplete", slog.String("err", err.Error()))
	} else {
		log.Info("metrics_stopped")
	}

	if err := apiSrv.Shutdown(shutdownCtx); err != nil {
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
