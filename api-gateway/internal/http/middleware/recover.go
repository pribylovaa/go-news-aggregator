package middleware

import (
	"fmt"
	"log/slog"
	"net/http"

	apierrors "github.com/pribylovaa/go-news-aggregator/api-gateway/internal/errors"
	logctx "github.com/pribylovaa/go-news-aggregator/pkg/log"
)

// Recover перехватывает panic, конвертирует в 500/internal и пишет унифицированный ответ.
// Детали паники не утекают на клиент.
func Recover() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// Безопасно логируем факт паники; детали наружу не отдаем.
					logctx.From(r.Context()).
						LogAttrs(r.Context(), slog.LevelError, "panic",
							slog.String("path", r.URL.Path),
							slog.Any("reason", rec),
						)
					apierrors.WriteError(w, r, fmt.Errorf("internal"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
