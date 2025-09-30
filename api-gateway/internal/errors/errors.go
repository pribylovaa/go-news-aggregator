// errors стандартизирует ответы об ошибках HTTP-слоя api-gateway.
// На вход он принимает ошибку (gRPC-статус от апстрим-сервисов),
// а на выход даёт:
//   - корректный HTTP-статус;
//   - краткое безопасное message без утечки деталей.
//
// Источник истинности по маппингу: транспортные слои сервисов.
//
// Поддержка доменных кодов через google.rpc.ErrorInfo:
// На текущий момент сервисы возвращают только gRPC codes (без ErrorInfo).
package errors

import (
	"encoding/json"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Нестандартный код часто используемый для "клиент закрыл соединение".
const StatusClientClosedRequest = 499

// APIError — единый формат для фронта.
// Code — короткий стабильный код для машиночитаемой обработки на FE.
// Message — безопасное человекочитаемое описание.
// RequestID — прокидывается из X-Request-Id, если есть (для трассировки).
type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// ErrorResponse — корневой объект в ответе.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// ToHTTP конвертирует входную ошибку (обычно gRPC-статус от апстрима)
// в HTTP-статус и унифицированный ответ для фронта.
//
// Поведение:
//   - err == nil - это программная ошибка вызова: возвращаем 500/internal,
//     чтобы не послать "200 OK" с телом ошибки и не маскировать баг.
//   - err — не gRPC-статус - 500/internal (без утечки деталей).
//   - err — gRPC-статус - маппим codes.Code через baseFromGRPC()
//     (InvalidArgument -> 400, NotFound -> 404, AlreadyExists -> 409, и т.д.).
func ToHTTP(err error) (int, ErrorResponse) {
	if err == nil {
		return http.StatusInternalServerError, ErrorResponse{
			Error: APIError{
				Code:    "internal",
				Message: "internal error",
			},
		}
	}

	st, ok := status.FromError(err)
	if !ok {
		return http.StatusInternalServerError, ErrorResponse{
			Error: APIError{
				Code:    "internal",
				Message: "internal error",
			},
		}
	}

	httpStatus, code, msg := baseFromGRPC(st.Code())
	return httpStatus, ErrorResponse{
		Error: APIError{
			Code:    code,
			Message: msg,
		},
	}
}

// WriteError — хелпер для HTTP-хендлеров.
// Пишет корректный статус/тело, добавляет request_id из заголовка, если он есть.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, resp := ToHTTP(err)

	// Прокидываем request_id для фронта, чтобы он мог репортить баги с привязкой.
	if rid := r.Header.Get("X-Request-Id"); rid != "" {
		resp.Error.RequestID = rid
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// baseFromGRPC — базовый маппинг gRPC -> HTTP/FE-код/сообщение.
// Таблица учитывает реальные коды из транспортов сервисов:
//   - InvalidArgument (битые входные/курсор/UUID) -> 400
//   - NotFound -> 404
//   - AlreadyExists (конфликты уникальности/дубликаты) -> 409
//   - FailedPrecondition (логические ограничения: thread expired / max depth) -> 412
//   - Unauthenticated -> 401 (auth: invalid credentials/token/expired/revoked)
//   - PermissionDenied -> 403 (зарезервировано на будущее)
//   - ResourceExhausted -> 429 (rate limit/квоты; зарезервировано)
//   - Aborted -> 409 (конфликт транзакции; зарезервировано)
//   - Canceled -> 499 (клиент закрыл соединение)
//   - DeadlineExceeded -> 504 (таймаут запроса к апстриму)
//   - Unavailable -> 503 (апстрим недоступен)
//   - Unimplemented -> 501
//   - прочее -> 500/internal
func baseFromGRPC(c codes.Code) (int, string, string) {
	switch c {
	case codes.InvalidArgument:
		return http.StatusBadRequest, "invalid_argument", "invalid argument"
	case codes.NotFound:
		return http.StatusNotFound, "not_found", "not found"
	case codes.AlreadyExists:
		return http.StatusConflict, "already_exists", "already exists"
	case codes.FailedPrecondition:
		return http.StatusPreconditionFailed, "failed_precondition", "failed precondition"
	case codes.Unauthenticated:
		return http.StatusUnauthorized, "unauthenticated", "unauthenticated"
	case codes.PermissionDenied:
		return http.StatusForbidden, "permission_denied", "permission denied"
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests, "resource_exhausted", "resource exhausted"
	case codes.Aborted:
		return http.StatusConflict, "aborted", "aborted"
	case codes.Canceled:
		return StatusClientClosedRequest, "canceled", "canceled"
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout, "deadline_exceeded", "deadline exceeded"
	case codes.Unavailable:
		return http.StatusServiceUnavailable, "unavailable", "service unavailable"
	case codes.Unimplemented:
		return http.StatusNotImplemented, "unimplemented", "unimplemented"
	default:
		return http.StatusInternalServerError, "internal", "internal error"
	}
}
