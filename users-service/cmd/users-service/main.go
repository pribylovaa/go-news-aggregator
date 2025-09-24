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

	"github.com/pribylovaa/go-news-aggregator/pkg/interceptors"
	usersv1 "github.com/pribylovaa/go-news-aggregator/users-service/gen/go/users"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/service"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage/minio"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage/postgres"
	usersgrpc "github.com/pribylovaa/go-news-aggregator/users-service/internal/transport/grpc"
	"google.golang.org/grpc"
	health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Константы для определения окружения.
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
	log.Info("starting users-service", "env", cfg.Env)

	rootCtx, rootCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	dbCtx, dbCancel := context.WithTimeout(rootCtx, 10*time.Second)
	profilesStore, err := postgres.New(dbCtx, cfg.Postgres.URL)
	dbCancel()
	if err != nil {
		log.Error("postgres_connect_failed", slog.String("err", err.Error()))
		rootCancel()
		os.Exit(1)
	}
	log.Info("postgres_connected")

	s3Ctx, s3Cancel := context.WithTimeout(rootCtx, 10*time.Second)
	avatarsStore, err := minio.New(s3Ctx, cfg)
	s3Cancel()
	if err != nil {
		log.Error("minio_connect_failed", slog.String("err", err.Error()))
		rootCancel()
		profilesStore.Close()
		os.Exit(1)
	}
	log.Info("minio_connected")

	svc := service.New(profilesStore, avatarsStore, cfg)
	log.Info("service_initialized")

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

	grpcOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			interceptors.Recover(log),
			interceptors.UnaryLoggingInterceptor(log),
			interceptors.WithTimeout(cfg.Timeouts.Service),
		),
	}
	grpcServer := grpc.NewServer(grpcOpts...)

	hs := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, hs)

	usersv1.RegisterUsersServiceServer(grpcServer, usersgrpc.NewUsersServer(svc))

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
		profilesStore.Close()
		os.Exit(1)
	}
	log.Info("grpc_listen_start", slog.String("addr", addr))

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
	profilesStore.Close()

	log.Info("service_stopped")
	os.Exit(0)
}

// setupLogger настраивает slog по окружению.
func setupLogger(env string) *slog.Logger {
	var log *slog.Logger

	switch env {
	case envLocal:
		log = slog.New(
			slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	case envDev:
		log = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	case envProd:
		log = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
		)
	default:
		log = slog.New(
			slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	}

	return log
}
