// storage определяет контракты доступа к БД для news-service.
package storage

import (
	"context"
	"errors"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
)

var (
	// ErrNotFound — сущность отсутствует в хранилище.
	ErrNotFound = errors.New("not found")
	// ErrInvalidCursor - битый/чужой page_token (курсор пагинации).
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrConflict — конфликт уникальности (например, по link), если политика не upsert.
	ErrConflict = errors.New("conflict")
)

// NewsStorage описывает операции над сущностью models.News.
type NewsStorage interface {
	// SaveNews сохраняет пачку новостей (ожидаемый сценарий — upsert по канонической ссылке link).
	// Возврат ErrConflict, если реализуем «жёсткую» уникальность без upsert.
	SaveNews(ctx context.Context, items []models.News) error
	// ListNews возвращает страницу новостей, отсортированных по published_at.
	// При некорректном page_token должна вернуться ошибка ErrInvalidCursor.
	ListNews(ctx context.Context, opts models.ListOptions) (*models.Page, error)
	// NewsByID возвращает новость по её строковому идентификатору (формат — деталь реализации).
	// Если запись не найдена — ErrNotFound.
	NewsByID(ctx context.Context, id string) (*models.News, error)
}

// Storage задаёт контракт доступа к хранилищу для news-сервиса.
type Storage interface {
	NewsStorage
	Close()
}
