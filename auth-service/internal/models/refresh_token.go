package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshToken описывает серверную запись о refresh-токене.
//
// Описание:
//   - RefreshTokenHash — хэш «сырого» refresh-токена: sha256 -> base64.RawURLEncoding;
//     хранится только хэш, сам токен остаётся у клиента;
//   - UserID — владелец токена;
//   - CreatedAt/ExpiresAt — временные метки в UTC; истечение определяется по ExpiresAt;
//   - Revoked — флаг отзыва токена (true, если токен больше недействителен независимо от срока).
type RefreshToken struct {
	// RefreshTokenHash — уникальный хэш refresh-токена.
	RefreshTokenHash string
	// UserID — идентификатор пользователя, которому принадлежит токен.
	UserID uuid.UUID
	// CreatedAt — время выпуска токена (UTC).
	CreatedAt time.Time
	// ExpiresAt — время истечения срока действия (UTC).
	ExpiresAt time.Time
	// Revoked — признак отзыва токена.
	Revoked bool
}
