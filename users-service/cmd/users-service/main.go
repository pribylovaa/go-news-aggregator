package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

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
