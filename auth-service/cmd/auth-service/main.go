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

	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/cache"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	authv1 "github.com/pribylovaa/go-news-aggregator/auth-service/gen/go/auth"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/service"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/storage/postgres"
	auth "github.com/pribylovaa/go-news-aggregator/auth-service/internal/transport/grpc"
	"github.com/pribylovaa/go-news-aggregator/pkg/interceptors"

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
	flag.StringVar(&configPath, "config", "", "path to config file")
	flag.Parse()

	cfg := config.MustLoad(configPath)

	log := setupLogger(cfg.Env)
	slog.SetDefault(log)
	log.Info("starting application", "env", cfg.Env)

	// Корневой контекст по сигналам.
	rootCtx, rootCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	// Подключение к БД c таймаутом.
	dbCtx, dbCancel := context.WithTimeout(rootCtx, 10*time.Second)
	str, err := postgres.New(dbCtx, cfg.DB.DatabaseURL)
	dbCancel()
	if err != nil {
		log.Error("postgres_connect_failed", slog.String("err", err.Error()))
		rootCancel()
		os.Exit(1)
	}
	log.Info("postgres_connected")

	// Сервис.
	srvc := service.New(str, cfg.Auth)
	log.Info("service_initialized")

	// Redis cache (optional best-effort)
	var rcache cache.RefreshCache
	if cfg.Redis.RedisURL != "" {
		c, err := cache.NewRedisCache(cfg.Redis.RedisURL, "auth:rt:")
		if err != nil {
			log.Warn("redis_connect_failed", slog.String("err", err.Error()))
		} else {
			rcache = c
			srvc.SetRefreshCache(rcache)
			log.Info("redis_connected")
		}
	}

	var ready int32 // 0 — not ready; 1 — ready
	httpAddr := cfg.HTTP.Addr()

	mux := http.NewServeMux()
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		// health-запрос проксируем в gRPC health-сервис (ниже он ставится в SERVING)
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
		log.Info("http_listen_start", slog.String("addr", httpAddr))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http_listen_failed", slog.String("err", err.Error()))
			rootCancel()
		}
	}()

	// gRPC сервер и интерсепторы.
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

	// Health-check сервис.
	hs := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, hs)

	// Регистрация сервиса.
	authv1.RegisterAuthServiceServer(grpcServer, auth.NewAuthServer(srvc))

	// Рефлексия — только в local/dev.
	if cfg.Env == envLocal || cfg.Env == envDev {
		reflection.Register(grpcServer)
	}

	// Фоновая очистка просроченных refresh-токенов.
	startRefreshJanitor(rootCtx, str, log, 30*time.Minute)

	// Старт gRPC-сервера.
	addr := cfg.GRPC.Addr()
	li, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("grpc_listen_failed",
			slog.String("addr", addr),
			slog.String("err", err.Error()),
		)
		rootCancel()
		if rcache != nil {
			_ = rcache.Close()
		}
		str.Close()
		os.Exit(1)
	}
	log.Info("grpc_listen_start", slog.String("addr", addr))

	grpc_prometheus.Register(grpcServer)

	// Сервис готов: health -> SERVING и readiness=1
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	atomic.StoreInt32(&ready, 1)

	serveErrCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(li); err != nil {
			serveErrCh <- err
		}
	}()

	// Ожидание завершения по сигналу или ошибке сервера.
	select {
	case <-rootCtx.Done():
		log.Info("shutdown_signal_received")
	case err := <-serveErrCh:
		log.Error("grpc_serve_failed", slog.String("err", err.Error()))
	}

	// Переводим health в NOT_SERVING.
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	atomic.StoreInt32(&ready, 0)

	// Грейсфул остановка gRPC с таймаутом.
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

	// Грейсфул остановка HTTP
	_ = httpSrv.Shutdown(context.Background())

	// Явная очистка перед выходом.
	shutdownCancel()
	rootCancel()
	if rcache != nil {
		_ = rcache.Close()
	}
	str.Close()

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
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
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

// startRefreshJanitor запускает фонового сборщика просроченных refresh-токенов.
func startRefreshJanitor(ctx context.Context, storage storage.RefreshTokenStorage, log *slog.Logger, period time.Duration) {
	go func() {
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := storage.DeleteExpiredTokens(ctx, time.Now().UTC()); err != nil {
					log.Error("refresh_janitor_failed", slog.String("err", err.Error()))
				}
			}
		}
	}()
}
