package interceptors

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// ClientWithTimeout возвращает unary-клиентский интерсептор, который навешивает
// таймаут d на исходящий gRPC-вызов, если у контекста ещё нет дедлайна.
// Существующий дедлайн не переопределяется.
//
// Контракт:
//  1. d <= 0 — не модифицирует контекст, просто вызывает invoker;
//  2. у ctx уже есть deadline — оставляет как есть;
//  3. иначе — оборачивает ctx через context.WithTimeout(ctx, d), гарантированно
//     вызывает cancel() и передаёт обёрнутый ctx дальше.
//
// Ошибки:
//
//	По истечении дедлайна invoker, как правило, вернёт context.DeadlineExceeded;
//	gRPC транслирует это в codes.DeadlineExceeded на стороне клиента.
func ClientWithTimeout(d time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if d <= 0 {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		if _, ok := ctx.Deadline(); ok {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		cctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()

		return invoker(cctx, method, req, reply, cc, opts...)
	}
}
