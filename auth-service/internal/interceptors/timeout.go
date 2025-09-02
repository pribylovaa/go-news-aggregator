// interceptors предоставляет набор gRPC-интерсепторов для серверной стороны.
package interceptors

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// WithTimeout возвращает unary-интерсептор, который навешивает таймаут d на контекст
// запроса при его отсутствии. Существующий дедлайн не переопределяется.
//
// Контракт:
//  1. d <= 0 — возвращает результат handler без изменения контекста;
//  2. deadline уже задан во входящем ctx — не модифицирует его;
//  3. иначе — оборачивает ctx через context.WithTimeout(ctx, d), гарантированно
//     вызывает cancel() и передаёт обёрнутый ctx в handler.
//
// Ошибки:
//
//	По истечении дедлайна handler обычно возвращает context.DeadlineExceeded;
//	gRPC-рантайм транслирует это в codes.DeadlineExceeded.
func WithTimeout(d time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if d <= 0 {
			return handler(ctx, req)
		}

		if _, ok := ctx.Deadline(); ok {
			return handler(ctx, req)
		}

		ctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()

		return handler(ctx, req)
	}
}
