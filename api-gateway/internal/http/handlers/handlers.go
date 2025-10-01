package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/clients"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Handlers агрегирует зависимости (grpc-клиенты).
type Handlers struct {
	Clients *clients.Clients
}

func New(c *clients.Clients) *Handlers {
	return &Handlers{Clients: c}
}

// writeJSON — единый ответ JSON с нужным Content-Type.
// Ошибки выводим через apierrors.WriteError.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// decodeStrict — строгий JSON-декодер: запрещаем неизвестные поля.
func decodeStrict(r *http.Request, value any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(value)
}

// statusErrorInvalidArgument — вспомогалка: локальная ошибка парсинга -> gRPC InvalidArgument.
func statusErrorInvalidArgument() error {
	return status.Error(codes.InvalidArgument, "invalid argument")
}
