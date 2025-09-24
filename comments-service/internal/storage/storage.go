package storage

import (
	"context"
	"errors"

	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/models"
)

var (
	// ErrNotFound — сущность отсутствует в хранилище.
	ErrNotFound = errors.New("not found")
	// ErrInvalidCursor — битый/чужой page_token.
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrConflict — конфликт уникальности.
	ErrConflict = errors.New("conflict")
	// ErrParentNotFound — указан parent_id, но родитель не найден.
	ErrParentNotFound = errors.New("parent not found")
	// ErrThreadExpired — ветка (корень) истекла по TTL, создание/изменение запрещено.
	ErrThreadExpired = errors.New("thread expired")
	// ErrMaxDepthExceeded — превышена максимально допустимая глубина.
	ErrMaxDepthExceeded = errors.New("max depth exceeded")
)

// Storage описывает операции над комментариями.
type Storage interface {
	// CreateComment создаёт корневой комментарий или ответ.
	// Входной Comment должен содержать:
	//   - NewsID, UserID, Username, Content (обязательные);
	//   - ParentID (опционально, если это ответ).
	// Игнорируемые/вычисляемые полями хранилища: ID, Level, RepliesCount, IsDeleted, CreatedAt, UpdatedAt, ExpiresAt.
	// Возможные ошибки: ErrParentNotFound, ErrThreadExpired, ErrMaxDepthExceeded, ErrConflict.
	CreateComment(ctx context.Context, comment models.Comment) (*models.Comment, error)

	// DeleteComment выполняет мягкое удаление (is_deleted=true) по идентификатору.
	// Если запись не найдена — ErrNotFound.
	DeleteComment(ctx context.Context, id string) error

	// CommentByID возвращает комментарий по его строковому идентификатору.
	// Если запись не найдена — ErrNotFound.
	CommentByID(ctx context.Context, id string) (*models.Comment, error)

	// ListByNews возвращает страницу корневых комментариев новости (parent_id == "").
	// Сортировка: сначала новые (created_at DESC).
	// При некорректном page_token — ErrInvalidCursor.
	ListByNews(ctx context.Context, newsID string, p models.ListParams) (*models.Page, error)

	// ListReplies возвращает страницу ответов для одной ветки (дети одного parent_id).
	// Сортировка: сначала старые (created_at ASC) — удобнее для постепенной подзагрузки.
	// При некорректном page_token — ErrInvalidCursor.
	ListReplies(ctx context.Context, parentID string, p models.ListParams) (*models.Page, error)

	// Close закрывает соединения/ресурсы хранилища.
	Close()
}
