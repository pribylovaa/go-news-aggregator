// recover.go реализует перехватчик паник для unary-вызовов.
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

// Recover возвращает unary-интерсептор, который перехватывает паники в обработчиках,
// логирует их и отвечает клиенту нейтральной ошибкой codes.Internal.
//
// Поведение:
//   - Паника в любом месте стека RPC приводит к логзаписи уровня Error с методом и стеком;
//   - В ответ клиенту уходит status.Error(codes.Internal, "internal server error")
//     без раскрытия внутренних деталей;
//   - Если в контексте уже есть логгер (см. pkg/log), будет использован он;
//     иначе — переданный base (если не nil), либо slog.Default().
func Recover(base *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		l := log.From(ctx)
		if l == slog.Default() && base != nil {
			l = base
		}

		defer func() {
			if r := recover(); r != nil {
				l.Error("panic_recovered",
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
