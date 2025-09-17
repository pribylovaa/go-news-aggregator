package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/pkg/log"
)

// StartIngest запускает периодический опрос источников из конфига s.cfg.Fetcher.
//
// Особенности:
//   - парсинг выполняется через переданный Parser, сохранение — через s.storage.SaveNews;
//   - останавливается по ctx.
func (s *Service) StartIngest(ctx context.Context, parser Parser) error {
	const op = "service/fetcher/StartIngest"

	src := s.cfg.Fetcher.Sources
	interval := s.cfg.Fetcher.Interval

	if len(src) == 0 {
		return fmt.Errorf("%s: no sources configured", op)
	}

	lg := log.From(ctx)
	lg.Info("ingest_start",
		slog.String("op", op),
		slog.Int("sources", len(src)),
		slog.Duration("interval", interval),
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := s.ingestOnce(ctx, parser, src); err != nil {
		lg.Warn("ingest_tick_error",
			slog.String("op", op),
			slog.String("err", err.Error()),
		)
	}

	for {
		select {
		case <-ctx.Done():
			lg.Info("ingest_stop", slog.String("op", op))
			return nil
		case <-ticker.C:
			if err := s.ingestOnce(ctx, parser, src); err != nil {
				lg.Warn("ingest_tick_error",
					slog.String("op", op),
					slog.String("err", err.Error()),
				)
			}
		}
	}
}

// ingestOnce — один проход: парсинг всех источников, валидация, сохранение.
func (s *Service) ingestOnce(ctx context.Context, parser Parser, urls []string) error {
	const op = "service/fetcher/ingestOnce"

	lg := log.From(ctx)
	now := time.Now().UTC()

	output := parser.ParseMany(ctx, urls)

	var total, feedsOK, feedsErr int
	var batch []models.News

	for result := range output {
		if result.Err != nil {
			feedsErr++
			lg.Warn("parse_error",
				slog.String("op", op),
				slog.String("url", result.URL),
				slog.String("err", result.Err.Error()),
			)
			continue
		}

		for _, item := range result.Items {
			if news, ok := finalizeNews(item, now); ok {
				batch = append(batch, news)
			}
		}

		total += len(result.Items)
		feedsOK++
	}

	if len(batch) == 0 {
		lg.Info("ingest_empty",
			slog.String("op", op),
			slog.Int("feeds_ok", feedsOK),
			slog.Int("feeds_err", feedsErr),
		)
		return nil
	}

	if err := s.storage.SaveNews(ctx, batch); err != nil {
		return fmt.Errorf("%s: save_news: %w", op, err)
	}

	lg.Info("ingest_saved",
		slog.String("op", op),
		slog.Int("total_items", total),
		slog.Int("saved", len(batch)),
		slog.Int("feeds_ok", feedsOK),
		slog.Int("feeds_err", feedsErr),
	)

	return nil
}
