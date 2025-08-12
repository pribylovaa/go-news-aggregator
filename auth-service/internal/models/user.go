package models

import (
	"time"

	"github.com/google/uuid"
)

// User - модель пользователя в системе.
type User struct {
	ID        uuid.UUID
	Email     string
	Password  string
	CreatedAt time.Time
	UpdatedAt time.Time
}
