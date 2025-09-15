// storage определяет контракты доступа к хранилищу данных для auth-сервиса.
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/models"

	"github.com/google/uuid"
)

var (
	// ErrNotFound возвращается, когда сущность отсутствует в хранилище
	// (например, пользователь с заданным email/ID или refresh-токен по хэшу).
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists возвращается при нарушении ограничений уникальности
	// (например, email пользователя или хэш refresh-токена уже заняты).
	ErrAlreadyExists = errors.New("already exists")
)

// UserStorage описывает операции над сущностью models.User.
//
// Ожидаемое поведение:
//   - SaveUser: создаёт нового пользователя. При конфликте уникальности возвращает ErrAlreadyExists.
//   - UserByEmail/UserByID: возвращают ErrNotFound, если пользователь не найден.
type UserStorage interface {
	// SaveUser создает нового пользователя в БД.
	SaveUser(ctx context.Context, user *models.User) error
	// UserByEmail находит пользователя по email.
	UserByEmail(ctx context.Context, email string) (*models.User, error)
	// UserByID находит пользователя по ID.
	UserByID(ctx context.Context, id uuid.UUID) (*models.User, error)
}

// RefreshTokenStorage описывает операции над refresh-токенами.
//
// Ожидаемое поведение:
//   - SaveRefreshToken: создаёт запись о токене; при конфликте уникальности по хэшу — ErrAlreadyExists.
//   - RefreshTokenByHash: возвращает токен или ErrNotFound — истечение срока/ревокация не конвертируются в ошибку.
//   - RevokeRefreshToken: если токен активен — помечает как отозванный и возвращает (true, nil);
//     если токен уже отозван — (false, nil); если токен не найден — (false, ErrNotFound).
//   - DeleteExpiredTokens: удаляет все токены с истёкшим сроком (ExpiresAt <= now);
//     рекомендуется передавать now в UTC.
type RefreshTokenStorage interface {
	// SaveRefreshToken сохраняет новый refresh-token в БД.
	SaveRefreshToken(ctx context.Context, token *models.RefreshToken) error
	// RefreshTokenByHash находит refresh-токен по его хэшу.
	RefreshTokenByHash(ctx context.Context, hash string) (*models.RefreshToken, error)
	// RevokeRefreshToken отзывает refresh-токен.
	RevokeRefreshToken(ctx context.Context, hash string) (bool, error)
	// DeleteExpiredTokens удаляет все просроченные токены на момент now.
	DeleteExpiredTokens(ctx context.Context, now time.Time) error
}

// Storage задаёт контракт доступа к хранилищу для auth-сервиса.
type Storage interface {
	UserStorage
	RefreshTokenStorage
	Close()
}
