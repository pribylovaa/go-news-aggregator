package storage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrNotFoundAvatar — объект (ключ) отсутствует в бакете.
	ErrNotFoundAvatar = errors.New("not found")
	// ErrInvalidArgument — нарушены ограничения запроса (тип/размер).
	ErrInvalidArgument = errors.New("invalid argument")
)

// UploadInfo — информация для клиента о presigned PUT загрузке.
//   - UploadURL: конечная URL для PUT-запроса.
//   - AvatarKey: ключ (путь) будущего объекта в бакете.
//   - Expires: время жизни подписи.
//   - RequiredHeader: заголовки, которые клиент ОБЯЗАН передать при PUT (например Content-Type).
type UploadInfo struct {
	UploadURL      string
	AvatarKey      string
	Expires        time.Duration
	RequiredHeader map[string]string
}

// Avatars — контракт генерации presigned URL и подтверждения факта загрузки.
type Avatars interface {
	// AvatarUploadURL генерирует presigned PUT. Внутри — валидация contentType и contentLength.
	AvatarUploadURL(ctx context.Context, userID uuid.UUID, contentType string, contentLength int64) (*UploadInfo, error)
	// CheckAvatarUpload - проверяет факт загрузки по key (наличие, тип, размер).
	// Возвращает публичный URL (если сконфигурирован PublicBaseURL) и финальный key.
	CheckAvatarUpload(ctx context.Context, userID uuid.UUID, key string) (publicURL string, err error)
}

// AvatarsStorage — алиас-обёртка для внедрения зависимости.
type AvatarsStorage interface {
	Avatars
}
