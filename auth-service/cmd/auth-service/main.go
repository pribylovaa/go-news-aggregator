package main

import (
	authv1 "auth-service/gen/go/auth"
	"auth-service/internal/config"
	"auth-service/internal/interceptors"
	"auth-service/internal/service"
	"auth-service/internal/storage"
	"auth-service/internal/storage/postgres"
	auth "auth-service/internal/transport/grpc"
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Контекст для graceful shutdown.
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Подключение к БД c таймаутом.
	dbCtx, dbCancel := context.WithTimeout(rootCtx, 10*time.Second)
	st, err := postgres.New(dbCtx, cfg.DB.DatabaseURL)
	dbCancel()
	if err != nil {
		log.Error("postgres_connect_failed", slog.String("err", err.Error()))
		os.Exit(1)
	}
	defer st.Close()
	log.Info("postgres_connected")

	// Сервис.
	svc := service.New(st, cfg.Auth)
	log.Info("service_initialized")

	// gRPC-сервер и интерсепторы.
	grpcOpts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			interceptors.Recover(log),
			interceptors.UnaryLoggingInterceptor(log),
			interceptors.WithTimeout(cfg.Timeouts.Service),
		),
	}
	grpcServer := grpc.NewServer(grpcOpts...)

	// Регистрация сервиса.
	authv1.RegisterAuthServiceServer(grpcServer, auth.NewAuthServer(svc))

	// Рефлексия — только в local/dev.
	if cfg.Env == envLocal || cfg.Env == envDev {
		reflection.Register(grpcServer)
	}

	// Фоновая очистка просроченных refresh-токенов.
	startRefreshJanitor(rootCtx, st, log, 30*time.Minute)

	// Старт сервера.
	addr := cfg.GRPC.Addr()
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

	// Ожидаем сигнал завершения или фатальную ошибку сервера.
	select {
	case <-rootCtx.Done():
		log.Info("shutdown_requested")
	case err := <-serveErrCh:
		if err != nil {
			log.Error("grpc_serve_failed", slog.String("err", err.Error()))
		}
	}

	// Graceful stop с таймаутом.
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

// startRefreshJanitor запускает фоновую задачу, которая периодически удаляет
// просроченные refresh-токены из хранилища с помощью storage.DeleteExpiredTokens.
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
