package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/storage"

	"github.com/pribylovaa/go-news-aggregator/auth-service/pkg/log"
)

// ListNews возвращает страницу новостей с нормализацией лимита по конфигу.
//
// Правила нормализации:
// - limit <= 0 -> cfg.LimitsConfig.Default;
// - limit > max -> cfg.LimitsConfig.Max;
// - пустой pageToken -> первая страница.
//
// Ошибки:
// - ErrInvalidCursor — битый/чужой page_token (маппинг storage.ErrInvalidCursor);
// - прочие ошибки стораджа — обёрнутые и прокинуты наверх.
func (s *Service) ListNews(ctx context.Context, opts models.ListOptions) (*models.Page, error) {
	const op = "service.queries.ListNews"

	lg := log.From(ctx)
	lg.Info("list_news_request",
		slog.String("op", op),
		slog.Int("limit", int(opts.Limit)),
		slog.Bool("has_page_token", opts.PageToken != ""),
	)

	if opts.Limit <= 0 {
		opts.Limit = s.cfg.LimitsConfig.Default
	}

	if s.cfg.LimitsConfig.Max > 0 && opts.Limit > s.cfg.LimitsConfig.Max {
		opts.Limit = s.cfg.LimitsConfig.Max
	}

	page, err := s.storage.ListNews(ctx, opts)
	if err != nil {
		if errors.Is(err, storage.ErrInvalidCursor) {
			lg.Warn("list_news_invalid_cursor",
				slog.String("op", op),
			)

			return nil, fmt.Errorf("%s: %w", op, ErrInvalidCursor)
		}

		lg.Error("list_news_storage_error",
			slog.String("op", op),
			slog.String("err", err.Error()),
		)

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	lg.Info("list_news_ok",
		slog.String("op", op),
		slog.Int("items", len(page.Items)),
		slog.Bool("has_next_page", page.NextPageToken != ""),
	)

	return page, nil
}

// NewsByID возвращает новость по идентификатору.
//
// Ошибки:
// - ErrNotFound — если запись отсутствует (маппинг storage.ErrNotFound);
// - прочие ошибки стораджа — обёрнутые и прокинуты наверх.
func (s *Service) NewsByID(ctx context.Context, id string) (*models.News, error) {
	const op = "service.queries.NewsByID"

	lg := log.From(ctx)
	lg.Info("news_by_id_request",
		slog.String("op", op),
		slog.String("id", id),
	)

	news, err := s.storage.NewsByID(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			lg.Warn("news_by_id_not_found",
				slog.String("op", op),
				slog.String("id", id),
			)

			return nil, fmt.Errorf("%s: %w", op, ErrNotFound)
		}

		lg.Error("news_by_id_storage_error",
			slog.String("op", op),
			slog.String("id", id),
			slog.String("err", err.Error()),
		)

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	lg.Info("news_by_id_ok",
		slog.String("op", op),
		slog.String("id", id),
	)

	return news, nil
}
