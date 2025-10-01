// interceptors предоставляет набор gRPC-интерсепторов для клиентской стороны.
package interceptors

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/pkg/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ClientUnaryLoggingInterceptor — логирование исходящих unary-вызовов.
// Поведение:
//   - вытягивает x-request-id из исходящего metadata (или генерирует новый и добавляет);
//   - добавляет поля method/target, прокладывает обогащённый логгер в контекст (pkg/log);
//   - пишет одну финальную запись уровня Info: msg="grpc", code, dur.
//
// Безопасность: не логирует payload и чувствительные заголовки.
func ClientUnaryLoggingInterceptor(base *slog.Logger) grpc.UnaryClientInterceptor {
	if base == nil {
		base = slog.Default()
	}

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()

		// request_id из исходящего metadata; если нет — создаём и добавляем.
		var rid string
		if md, ok := metadata.FromOutgoingContext(ctx); ok {
			if v := md.Get("x-request-id"); len(v) > 0 && v[0] != "" {
				rid = v[0]
			}
		}
		if rid == "" {
			rid = uuid.NewString()
			ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", rid)
		}

		// target (адрес апстрима) — полезно для трассировки.
		target := "-"
		if cc != nil && cc.Target() != "" {
			target = cc.Target()
		}

		// Обогащённый логгер и прокладка в контекст.
		l := base.With(
			slog.String("request_id", rid),
			slog.String("method", method),
			slog.String("target", target),
		)
		ctx = log.Into(ctx, l)

		// Выполнение вызова.
		err := invoker(ctx, method, req, reply, cc, opts...)

		// Итоговая запись в стиле сервисного интерсептора.
		l.Info("grpc",
			slog.String("code", status.Code(err).String()),
			slog.Duration("dur", time.Since(start)),
		)

		return err
	}
}
