package middleware

import (
	"net/http"
)

// Middleware — стандартный net/http мидлвар.
type Middleware func(http.Handler) http.Handler

// Chain применяет мидлвары к обработчику в порядке их перечисления.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// statusWriter оборачивает ResponseWriter, чтобы перехватить статус и размер.
type statusWriter struct {
	http.ResponseWriter
	status int
	count  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}

	count, err := w.ResponseWriter.Write(p)
	w.count += count
	return count, err
}

func newStatusWriter(w http.ResponseWriter) *statusWriter {
	return &statusWriter{ResponseWriter: w}
}
