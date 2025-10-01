package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/clients/interceptors"
)

// RequestID обеспечивает наличие X-Request-Id:
//  1. читает заголовок X-Request-Id, если есть;
//  2. иначе генерирует криптографически стойкий hex id (32 символа);
//  3. кладёт id в Response Header, Request Header (для удобства) и в контекст
//     по ключу interceptors.CtxRequestID (его читает gRPC client metadata-интерсептор).
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if id == "" {
				id = genID()
				// добавим в запрос — чтобы errors.WriteError мог его забрать.
				r.Header.Set("X-Request-Id", id)
			}
			w.Header().Set("X-Request-Id", id)

			ctx := context.WithValue(r.Context(), interceptors.CtxRequestID, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func genID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
