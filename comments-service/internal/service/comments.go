package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/pkg/log"

	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/storage"
)

// Входные структуры сервисного слоя.

// CreateCommentInput — создание корневого комментария или ответа.
// Правила:
//   - если ParentID пуст, создаётся корень и обязателен NewsID;
//   - если ParentID не пуст, создаётся ответ; NewsID можно не передавать
//     (слой storage унаследует news_id/ttl от родителя);
//   - всегда обязательны: UserID, Username, Content.
type CreateCommentInput struct {
	NewsID   uuid.UUID
	ParentID string
	UserID   uuid.UUID
	Username string
	Content  string
}

// ListByNewsInput — параметры постраничной выдачи корней по новости.
type ListByNewsInput struct {
	NewsID    uuid.UUID
	PageSize  int32
	PageToken string
}

// ListRepliesInput — параметры постраничной выдачи ответов по parent_id.
type ListRepliesInput struct {
	ParentID  string
	PageSize  int32
	PageToken string
}

// CreateComment — бизнес-операция создания комментария.
//
// Валидация:
//   - UserID обязателен (uuid.Nil -> ErrInvalidArgument);
//   - Username и Content нормализуются (TrimSpace) и не должны быть пустыми;
//   - Если ParentID пуст (создание корня) — NewsID обязателен (uuid.Nil -> ErrInvalidArgument).
//
// Поведение/ошибки:
//   - ErrParentNotFound — если указан ParentID, но родитель отсутствует;
//   - ErrThreadExpired — если истёк TTL ветки (корня);
//   - ErrMaxDepthExceeded — если превышена максимальная глубина;
//   - ErrConflict — конфликт уникальности;
//   - ErrInternal — прочие ошибки стораджа/БД/контекста.
func (s *Service) CreateComment(ctx context.Context, in CreateCommentInput) (*models.Comment, error) {
	const op = "service/comments/CreateComment"

	lg := log.From(ctx).With(
		"op", op,
		"user_id", in.UserID.String(),
		"news_id", in.NewsID.String(),
		"parent_id", in.ParentID,
	)

	// Валидация базовых атрибутов.
	if in.UserID == uuid.Nil {
		lg.Warn("invalid argument: empty user_id")
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" {
		lg.Warn("invalid argument: empty username")
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" {
		lg.Warn("invalid argument: empty content")
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	// Для корня обязательна привязка к новости.
	if strings.TrimSpace(in.ParentID) == "" && in.NewsID == uuid.Nil {
		lg.Warn("invalid argument: empty news_id for root comment")
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	comm := models.Comment{
		NewsID:   in.NewsID,
		ParentID: strings.TrimSpace(in.ParentID),
		UserID:   in.UserID,
		Username: in.Username,
		Content:  in.Content,
	}

	result, err := s.storage.CreateComment(ctx, comm)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrParentNotFound):
			lg.Warn("parent not found")
			return nil, fmt.Errorf("%s: %w", op, ErrParentNotFound)
		case errors.Is(err, storage.ErrThreadExpired):
			lg.Warn("thread expired")
			return nil, fmt.Errorf("%s: %w", op, ErrThreadExpired)
		case errors.Is(err, storage.ErrMaxDepthExceeded):
			lg.Warn("max depth exceeded")
			return nil, fmt.Errorf("%s: %w", op, ErrMaxDepthExceeded)
		case errors.Is(err, storage.ErrConflict):
			lg.Warn("conflict")
			return nil, fmt.Errorf("%s: %w", op, ErrConflict)
		default:
			lg.Error("storage error on CreateComment", "err", err)
			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return result, nil
}

// DeleteComment — мягкое удаление комментария по ID.
//
// Валидация:
//   - id не должен быть пустым.
//
// Поведение/ошибки:
//   - ErrNotFound — если комментарий не найден;
//   - ErrInternal — иные ошибки стораджа.
func (s *Service) DeleteComment(ctx context.Context, id string) error {
	const op = "service/comments/DeleteComment"

	id = strings.TrimSpace(id)
	lg := log.From(ctx).With("op", op, "id", id)

	if id == "" {
		lg.Warn("invalid argument: empty id")
		return fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	if err := s.storage.DeleteComment(ctx, id); err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			lg.Warn("comment not found")
			return fmt.Errorf("%s: %w", op, ErrNotFound)
		default:
			lg.Error("storage error on DeleteComment", "err", err)
			return fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return nil
}

// CommentByID — получить комментарий по ID.
//
// Валидация:
//   - id не должен быть пустым.
//
// Поведение/ошибки:
//   - ErrNotFound — если комментарий не найден (включая неверный формат идентификатора);
//   - ErrInternal — иные ошибки стораджа.
func (s *Service) CommentByID(ctx context.Context, id string) (*models.Comment, error) {
	const op = "service/comments/CommentByID"

	id = strings.TrimSpace(id)
	lg := log.From(ctx).With("op", op, "id", id)

	if id == "" {
		lg.Warn("invalid argument: empty id")
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	result, err := s.storage.CommentByID(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFound):
			lg.Warn("comment not found")
			return nil, fmt.Errorf("%s: %w", op, ErrNotFound)
		default:
			lg.Error("storage error on CommentByID", "err", err)
			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return result, nil
}

// ListByNews — страница корневых комментариев по идентификатору новости.
//
// Валидация:
//   - newsID обязателен (uuid.Nil -> ErrInvalidArgument).
//
// Поведение/ошибки:
//   - ErrInvalidCursor — если некорректный page_token;
//   - ErrInternal — иные ошибки стораджа.
func (s *Service) ListByNews(ctx context.Context, in ListByNewsInput) (*models.Page, error) {
	const op = "service/comments/ListByNews"

	lg := log.From(ctx).With("op", op, "news_id", in.NewsID.String())

	if in.NewsID == uuid.Nil {
		lg.Warn("invalid argument: empty news_id")
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	page, err := s.storage.ListByNews(ctx, in.NewsID.String(), models.ListParams{
		PageSize:  in.PageSize,
		PageToken: in.PageToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrInvalidCursor):
			lg.Warn("invalid cursor")
			return nil, fmt.Errorf("%s: %w", op, ErrInvalidCursor)
		default:
			lg.Error("storage error on ListByNews", "err", err)
			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return page, nil
}

// ListReplies — страница ответов в пределах одной ветки по parent_id.
//
// Валидация:
//   - parentID обязателен (пустой -> ErrInvalidArgument).
//
// Поведение/ошибки:
//   - ErrInvalidCursor — если некорректный page_token;
//   - ErrInternal — иные ошибки стораджа.
func (s *Service) ListReplies(ctx context.Context, in ListRepliesInput) (*models.Page, error) {
	const op = "service/comments/ListReplies"

	in.ParentID = strings.TrimSpace(in.ParentID)
	lg := log.From(ctx).With("op", op, "parent_id", in.ParentID)

	if in.ParentID == "" {
		lg.Warn("invalid argument: empty parent_id")
		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	page, err := s.storage.ListReplies(ctx, in.ParentID, models.ListParams{
		PageSize:  in.PageSize,
		PageToken: in.PageToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrInvalidCursor):
			lg.Warn("invalid cursor")
			return nil, fmt.Errorf("%s: %w", op, ErrInvalidCursor)
		default:
			lg.Error("storage error on ListReplies", "err", err)
			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return page, nil
}
