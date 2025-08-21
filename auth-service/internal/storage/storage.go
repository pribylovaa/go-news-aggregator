package storage

import (
	"auth-service/internal/models"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrNotFound — запись не найдена (пользователь/токен).
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists — нарушение уникальности (email/refresh-token).
	ErrAlreadyExists = errors.New("already exists")
	// ErrExpired — сущность просрочена (refresh-token).
	ErrExpired = errors.New("expired")
	// ErrRevoked — сущность отозвана (refresh-token).
	ErrRevoked = errors.New("revoked")
)

// UserStorage выполняет операции над пользователями.
type UserStorage interface {
	// SaveUser создает нового пользователя в БД.
	SaveUser(ctx context.Context, user *models.User) error
	// UserByEmail находит пользователя по email.
	UserByEmail(ctx context.Context, email string) (*models.User, error)
	// UserByID находит пользователя по ID.
	UserByID(ctx context.Context, id uuid.UUID) (*models.User, error)
}

// RefreshTokenStorage выполняет операции над refresh-токенами.
type RefreshTokenStorage interface {
	// SaveRefreshToken сохраняет новый refresh-token в БД.
	SaveRefreshToken(ctx context.Context, token *models.RefreshToken) error
	// RefreshTokenByHash находит refresh-токен по его хэшу.
	RefreshTokenByHash(ctx context.Context, hash string) (*models.RefreshToken, error)
	// RevokeRefreshToken пытается отозвать refresh-токен.
	RevokeRefreshToken(ctx context.Context, hash string) (bool, error)
	// DeleteExpiredTokens удаляет все просроченные токены.
	DeleteExpiredTokens(ctx context.Context, now time.Time) error
}

// Storage задает контракт работы с БД.
type Storage interface {
	UserStorage
	RefreshTokenStorage
	Close()
}
