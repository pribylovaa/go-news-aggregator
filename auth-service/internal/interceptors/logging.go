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

// UnaryLoggingInterceptor реализует логирование unary-вызовов с контекстным логгером.
//
// Поведение и формат логов:
//   - Вытягивает x-request-id из входящего metadata, иначе генерирует UUID;
//   - Извлекает peer (IP:port клиента), метод (FullMethod);
//   - Кладёт обогащённый *slog.Logger в context (pkg/log), чтобы он был доступен глубже по стеку;
//   - После выполнения handler пишет одну строку уровня Info: msg="grpc",
//     code=<gRPC status>, dur=<время выполнения>.
//
// Безопасность:
//   - Логи не содержат чувствительных данных (только метод/peer/request_id);
//   - Если базовый логгер не передан, используется slog.Default() (без паник);
//   - Контекст запроса не теряется: дедлайны/отмена и metadata сохраняются.
func UnaryLoggingInterceptor(base *slog.Logger) grpc.UnaryServerInterceptor {
	if base == nil {
		base = slog.Default()
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		// request_id: из metadata, иначе генерируется новый.
		var rid string
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if v := md.Get("x-request-id"); len(v) > 0 && v[0] != "" {
				rid = v[0]
			}
		}
		if rid == "" {
			rid = uuid.NewString()
		}

		// peer: IP:port или "-" если недоступно.
		peerStr := "-"
		if p, ok := peer.FromContext(ctx); ok && p != nil && p.Addr != nil {
			peerStr = p.Addr.String()
		}

		// обогащённый логгер и прокладка в контекст.
		l := base.With(
			slog.String("request_id", rid),
			slog.String("method", info.FullMethod),
			slog.String("peer", peerStr),
		)
		ctx = log.Into(ctx, l)

		resp, err := handler(ctx, req)

		// итоговая запись.
		l.Info("grpc",
			slog.String("code", status.Code(err).String()),
			slog.Duration("dur", time.Since(start)),
		)

		return resp, err
	}
}
