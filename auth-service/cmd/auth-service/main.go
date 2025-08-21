package main

import (
	"auth-service/internal/config"
	"auth-service/internal/interceptors"
	"auth-service/internal/service"
	"auth-service/internal/storage"
	"auth-service/internal/storage/postgres"
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	authv1 "auth-service/gen/go/auth"
	auth "auth-service/internal/transport/grpc"

	"google.golang.org/grpc"
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
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.Parse()

	cfg := config.MustLoad(configPath)

	log := setupLogger(cfg.Env)
	slog.SetDefault(log)
	log.Info("starting application", "env", cfg.Env)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbCtx, dbCancel := context.WithTimeout(rootCtx, 10*time.Second)
	st, err := postgres.New(dbCtx, cfg.DB.DatabaseURL)
	dbCancel()
	if err != nil {
		log.Error("postgres_connect_failed", slog.String("err", err.Error()))
		os.Exit(1)
	}
	defer st.Close()
	log.Info("postgres_connected")

	svc := service.New(st, cfg.Auth)
	log.Info("service_initialized")

	grpcOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			interceptors.WithTimeout(cfg.Timeouts.Service),
			interceptors.UnaryLoggingInterceptor(log),
			interceptors.Recover(log),
		),
	}
	grpcServer := grpc.NewServer(grpcOpts...)

	authv1.RegisterAuthServiceServer(grpcServer, auth.NewAuthServer(svc))

	if cfg.Env == envLocal || cfg.Env == envDev {
		reflection.Register(grpcServer)
	}

	startRefreshJanitor(rootCtx, st, log, 30*time.Minute)

	addr := net.JoinHostPort(cfg.GRPC.Host, cfg.GRPC.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("grpc_listen_failed",
			slog.String("addr", addr),
			slog.String("err", err.Error()),
		)
		os.Exit(1)
	}
	log.Info("grpc_listen_start", slog.String("addr", addr))

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

	log.Info("service_stopped")
}

// setupLogger настраивает логгер.
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

// startRefreshJanitor периодически вызывает DeleteExpiredTokens.
func startRefreshJanitor(ctx context.Context, st storage.Storage, log *slog.Logger, period time.Duration) {
	if period <= 0 {
		return
	}

	go func() {
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := st.DeleteExpiredTokens(ctx, time.Now().UTC()); err != nil {
					log.Error("refresh_janitor_failed", slog.String("err", err.Error()))
				}
			}
		}
	}()
}
