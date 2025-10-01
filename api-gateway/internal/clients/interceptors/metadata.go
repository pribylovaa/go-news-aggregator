package interceptors

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type CtxKey string

const (
	CtxRequestID CtxKey = "request_id"
	CtxAuthToken CtxKey = "auth_token"
)

// ClientWithMetadata — добавляет в исходящий gRPC вызов заголовки:
//   - x-request-id (если есть в контексте),
//   - authorization: Bearer <token> (если есть в контексте),
//   - user-agent (если передан параметром).
func ClientWithMetadata(userAgent string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		var pairs []string

		if v := ctx.Value(CtxRequestID); v != nil {
			if rid, _ := v.(string); rid != "" {
				pairs = append(pairs, "x-request-id", rid)
			}
		}
		if v := ctx.Value(CtxAuthToken); v != nil {
			if tok, _ := v.(string); tok != "" {
				pairs = append(pairs, "authorization", "Bearer "+tok)
			}
		}
		if userAgent != "" {
			pairs = append(pairs, "user-agent", userAgent)
		}
		if len(pairs) > 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, pairs...)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
