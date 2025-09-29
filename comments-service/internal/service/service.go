// service содержит бизнес-логику comments-сервиса.
package service

import (
	"errors"

	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/storage"
)

var (
	// ErrNotFound — сущность отсутствует в хранилище.
	ErrNotFound = errors.New("not found")
	// ErrInvalidCursor — битый/чужой page_token.
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrConflict — конфликт уникальности.
	ErrConflict = errors.New("conflict")
	// ErrParentNotFound — родитель не найден.
	ErrParentNotFound = errors.New("parent not found")
	// ErrThreadExpired — ветка (корень) истекла по TTL, создание/изменение запрещено.
	ErrThreadExpired = errors.New("thread expired")
	// ErrMaxDepthExceeded — превышена максимально допустимая глубина.
	ErrMaxDepthExceeded = errors.New("max depth exceeded")
	// ErrInvalidArgument — неверные входные параметры запроса к сервису.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrInternal — внутренняя ошибка (стораж/БД/контекст/и т.д.).
	ErrInternal = errors.New("internal")
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
