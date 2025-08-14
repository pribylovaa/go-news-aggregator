package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshToken - данные refresh-токена для управления сессиями.
type RefreshToken struct {
	RefreshTokenHash string
	UserID           uuid.UUID
	CreatedAt        time.Time
	ExpiresAt        time.Time
	Revoked          bool
}
