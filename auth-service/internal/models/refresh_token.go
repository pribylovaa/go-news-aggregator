package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshToken - данные refresh-токена для управления сессиями.
type RefreshToken struct {
	RefreshToken string
	UserID       uuid.UUID
	IssuedAt     time.Time
	ExpiresAt    time.Time
	Revoked      bool
}
