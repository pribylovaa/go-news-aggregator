package interceptors

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

func WithTimeout(d time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, ok := ctx.Deadline(); ok {
			return handler(ctx, req)
		}

		ctx, cancel := context.WithTimeout(ctx, d)

		defer cancel()
		return handler(ctx, req)
	}
}
