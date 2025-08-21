package interceptors

import (
	"auth-service/internal/pkg/log"
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func UnaryLoggingInterceptor(base *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		md, _ := metadata.FromIncomingContext(ctx)
		var rid string
		if v := md.Get("x-request-id"); len(v) > 0 && v[0] != "" {
			rid = v[0]
		} else {
			rid = uuid.NewString()
		}

		var peerStr string
		if p, ok := peer.FromContext(ctx); ok && p != nil && p.Addr != nil {
			peerStr = p.Addr.String()
		} else {
			peerStr = "-"
		}

		l := base.With(
			slog.String("request_id", rid),
			slog.String("method", info.FullMethod),
			slog.String("peer", peerStr),
		)

		ctx = log.Into(ctx, l)

		resp, err := handler(ctx, req)

		l.Info("grpc",
			slog.String("code", status.Code(err).String()),
			slog.Duration("dur", time.Since(start)),
		)

		return resp, err
	}
}
