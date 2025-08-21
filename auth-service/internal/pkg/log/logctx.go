package log

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

// Into кладёт логгер в контекст.
func Into(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// From достаёт логгер из контекста (или возвращает slog.Default()).
func From(ctx context.Context) *slog.Logger {
	if v := ctx.Value(ctxKey{}); v != nil {
		if l, ok := v.(*slog.Logger); ok && l != nil {
			return l
		}
	}

	return slog.Default()
}
