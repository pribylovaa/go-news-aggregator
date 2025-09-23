// storage содержит контракты слоя хранилищ users-service.
//
// profiles.go - работа с профилями в БД (создание/чтение/частичное обновление)
// и фиксация атрибутов аватара после успешного подтверждения загрузки в S3/MinIO.
// avatars.go - контракт для работы с загрузкой аватаров в S3/MinIO.
package storage

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/models"
)

var (
	// ErrNotFoundProfile — профиль не найден.
	ErrNotFoundProfile = errors.New("not found")
	// ErrAlreadyExists — профиль с тем же первичным ключом/уникальным полем уже существует.
	ErrAlreadyExists = errors.New("already exists")
)

// ProfileUpdate — частичный апдейт профиля.
// Параметры задаются pointer-полями: только непустые указатели обновляются в БД.
type ProfileUpdate struct {
	Username *string
	Age      *uint32
	Country  *string
	Gender   *models.Gender
}

// Profile — контракт репозитория профилей.
type Profile interface {
	// CreateProfile создаёт новый профиль.
	CreateProfile(ctx context.Context, profile *models.Profile) (*models.Profile, error)
	// ProfileByID возвращает профиль по user_id.
	ProfileByID(ctx context.Context, userID uuid.UUID) (*models.Profile, error)
	// UpdateProfile выполняет частичное обновление полей, указанных в update.
	// Реализация должна обновить updated_at.
	UpdateProfile(ctx context.Context, userID uuid.UUID, update ProfileUpdate) (*models.Profile, error)
	// ConfirmAvatarUpload фиксирует новый avatar_key и (опционально) avatar_url в записи профиля.
	// Необходимо вызвать после успешного подтверждения загрузки в S3/MinIO.
	ConfirmAvatarUpload(ctx context.Context, userID uuid.UUID, key, publicURL string) (*models.Profile, error)
}

// ProfilesStorage — верхнеуровневый интерфейс хранилища профилей.
type ProfilesStorage interface {
	Profile
	Close()
}
