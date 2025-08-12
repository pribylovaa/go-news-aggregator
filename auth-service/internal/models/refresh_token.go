package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshTokenMeta - метаданные refresh-токена для управления сессиями.
type RefreshTokenMeta struct {
	RefreshToken string
	UserID       uuid.UUID
	IssuedAt     time.Time
	ExpiresAt    time.Time
	Revoked      bool
}
