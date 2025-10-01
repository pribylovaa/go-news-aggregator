package middleware

import (
	"context"
	"net/http"
	"time"
)

// Timeout навешивает deadline на запрос, если его ещё нет.
// Значение <=0 делает мидлвар no-op.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		// Если d<=0, возвращаем исходный handler без обёртки.
		if d <= 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := r.Context().Deadline(); ok {
				next.ServeHTTP(w, r) // уважаем существующий deadline.
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
