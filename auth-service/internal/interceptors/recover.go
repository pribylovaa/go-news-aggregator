package interceptors

import (
	"auth-service/internal/pkg/log"
	"context"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Recover перехватывает паники, логирует и отвечает кодом Internal.
func Recover(base *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				l := log.From(ctx)

				if l == slog.Default() && base != nil {
					l = base
				}

				l.Error("panic recovered",
					slog.String("method", info.FullMethod),
					slog.Any("panic", r),
					slog.String("stack", string(debug.Stack())),
				)

				err = status.Error(codes.Internal, "internal server error")
				resp = nil
			}
		}()

		return handler(ctx, req)
	}
}
