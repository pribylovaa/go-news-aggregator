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

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/pribylovaa/go-news-aggregator/pkg/interceptors"

	commentsv1 "github.com/pribylovaa/go-news-aggregator/comments-service/gen/go/comments"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/service"
	csmongo "github.com/pribylovaa/go-news-aggregator/comments-service/internal/storage/mongo"
	commentsgrpc "github.com/pribylovaa/go-news-aggregator/comments-service/internal/transport/grpc"

	"google.golang.org/grpc"
	health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Константы окружения (как в users-service)
const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to config file (overrides CONFIG_PATH env)")
	flag.Parse()

	cfg := config.MustLoad(configPath)

	log := setupLogger(cfg.Env)
	slog.SetDefault(log)
	log.Info("starting comments-service", "env", cfg.Env)

	rootCtx, rootCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	dbCtx, dbCancel := context.WithTimeout(rootCtx, 10*time.Second)
	mongoStore, err := csmongo.New(dbCtx, cfg)
	dbCancel()
	if err != nil {
		log.Error("mongo_connect_failed", slog.String("err", err.Error()))
		rootCancel()
		os.Exit(1)
	}
	log.Info("mongo_connected")

	svc := service.New(mongoStore, *cfg)
	log.Info("service_initialized")

	// HTTP readiness/liveness/metrics
	var ready int32 // 0 — not ready; 1 — ready
	httpAddr := cfg.HTTP.Addr()

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

	httpSrv := &http.Server{
		Addr:              httpAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("http_listen_start", "addr", httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http_serve_failed", slog.String("err", err.Error()))
		}
	}()

	grpc_prometheus.EnableHandlingTimeHistogram()

	grpcOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			interceptors.Recover(log),
			interceptors.UnaryLoggingInterceptor(log),
			interceptors.WithTimeout(cfg.Timeouts.Service),
			grpc_prometheus.UnaryServerInterceptor,
		),
		grpc.ChainStreamInterceptor(
			grpc_prometheus.StreamServerInterceptor,
		),
	}
	grpcServer := grpc.NewServer(grpcOpts...)

	hs := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, hs)

	commentsv1.RegisterCommentsServiceServer(grpcServer, commentsgrpc.NewCommentsServer(svc))

	if cfg.Env == envLocal || cfg.Env == envDev {
		reflection.Register(grpcServer)
	}

	addr := cfg.GRPC.Addr()
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("grpc_listen_failed",
			slog.String("addr", addr),
			slog.String("err", err.Error()),
		)
		rootCancel()
		_ = mongoStore.Close(context.Background())
		os.Exit(1)
	}
	log.Info("grpc_listen_start", slog.String("addr", addr))

	grpc_prometheus.Register(grpcServer)

	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	atomic.StoreInt32(&ready, 1)

	serveErrCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serveErrCh <- err
		}
		close(serveErrCh)
	}()

	select {
	case <-rootCtx.Done():
		log.Info("shutdown_requested")
	case err := <-serveErrCh:
		if err != nil {
			log.Error("grpc_serve_failed", slog.String("err", err.Error()))
		}
	}

	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	atomic.StoreInt32(&ready, 0)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		log.Info("grpc_stopped")
	case <-shutdownCtx.Done():
		log.Warn("grpc_force_stop")
		grpcServer.Stop()
	}

	shutdownCancel()
	_ = httpSrv.Shutdown(context.Background())

	rootCancel()
	_ = mongoStore.Close(context.Background())

	log.Info("service_stopped")
	os.Exit(0)
}

// setupLogger — тот же подход, что в users-service.
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
