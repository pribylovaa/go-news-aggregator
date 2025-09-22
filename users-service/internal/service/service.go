// service содержит бизнес-логику users-сервиса:
// - операции над профилем (чтение/создание/частичный апдейт);
// - работа с аватарами (выдача presigned URL и подтверждение загрузки).
package service

import (
	"errors"

	"github.com/pribylovaa/go-news-aggregator/users-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
)

var (
	// ErrInvalidArgument — некорректные входные данные (валидация, mask и т.п.).
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrNotFound — сущность не найдена.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists — конфликт уникальности/дубликат.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInternal — внутренняя ошибка сервиса.
	ErrInternal = errors.New("internal")
)

// Service — описывает бизнес-логику users-service.
type Service struct {
	cfg             *config.Config
	profilesStorage storage.ProfilesStorage
	avatarsStorage  storage.AvatarsStorage
}

// New создает новый экземпляр Service.
func New(profilesStorage storage.ProfilesStorage, avatarsStorage storage.AvatarsStorage, cfg *config.Config) *Service {
	return &Service{
		profilesStorage: profilesStorage,
		avatarsStorage:  avatarsStorage,
		cfg:             cfg,
	}
}
