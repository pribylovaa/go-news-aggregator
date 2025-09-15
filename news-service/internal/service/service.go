// service содержит бизнес-логику news-сервиса.
package service

import (
	"errors"
	"news-service/internal/config"
	"news-service/internal/storage"
)

var (
	// ErrNotFound — сущность отсутствует.
	// Транспорт: codes.NotFound.
	ErrNotFound = errors.New("not found")
	// ErrInvalidCursor — битый/чужой page_token.
	// Транспорт: codes.InvalidArgument.
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrInvalidArgument - некорректные входные аргументы.
	// Транспорт: codes.InvalidArgument.
	ErrInvalidArgument = errors.New("invalid argument")
)

// Service — описывает бизнес-логику news-service.
type Service struct {
	storage storage.Storage
	cfg     config.Config
}

// New создает новый экземпляр Service.
func New(storage storage.Storage, cfg config.Config) *Service {
	return &Service{
		storage: storage,
		cfg:     cfg,
	}
}
