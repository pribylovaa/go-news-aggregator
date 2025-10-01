package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/clients/interceptors"
)

// AuthBearer извлекает Bearer-токен из Authorization и кладёт "сырой" токен
// в контекст по ключу interceptors.CtxAuthToken.
func AuthBearer() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")

			if auth != "" {
				const prefix = "Bearer "
				if strings.HasPrefix(auth, prefix) && len(auth) > len(prefix) {
					token := strings.TrimSpace(auth[len(prefix):])

					if token != "" {
						ctx := context.WithValue(r.Context(), interceptors.CtxAuthToken, token)
						r = r.WithContext(ctx)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
