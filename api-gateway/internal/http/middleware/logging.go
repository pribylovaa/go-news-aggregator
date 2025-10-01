// internal/http/middleware/logging.go
package middleware

import (
	"log/slog"
	"net/http"
	"time"

	logctx "github.com/pribylovaa/go-news-aggregator/pkg/log"
)

func Logging(l *slog.Logger) Middleware {
	if l == nil {
		l = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// сформируем request-scoped логгер и положим в контекст
			reqLogger := l
			if rid := r.Header.Get("X-Request-Id"); rid != "" {
				reqLogger = reqLogger.With(slog.String("request_id", rid))
			}
			ctx := logctx.Into(r.Context(), reqLogger)
			r = r.WithContext(ctx)

			sw := newStatusWriter(w)
			start := time.Now()
			next.ServeHTTP(sw, r)
			dur := time.Since(start)

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Duration("dur", dur),
				slog.Int("bytes", sw.count),
			}

			// Достаём тот же логгер из контекста (уже с request_id) и пишем запись.
			logctx.From(r.Context()).LogAttrs(r.Context(), slog.LevelInfo, "http", attrs...)
		})
	}
}
