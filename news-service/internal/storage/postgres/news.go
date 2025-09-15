package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SaveNews сохраняет пачку новостей с upsert по канонической ссылке.
//
// Политика обновления:
//   - title — всегда обновляется, если пришёл другой;
//   - long_description — обновляется, только если пришёл непустой и длиннее текущего;
//   - image_url/category/short_description — обновляются, если пришли новые непустые значения;
//   - published_at — не меняется;
//   - fetched_at — обновляется всегда.
func (s *Storage) SaveNews(ctx context.Context, items []models.News) error {
	const op = "storage.postgres.SaveNews"

	if len(items) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, item := range items {
		batch.Queue(`
		INSERT INTO news (title, category, short_description, long_description, link, image_url, published_at, fetched_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (link) DO UPDATE 
		SET 
		title = EXCLUDED.title,
		image_url = CASE WHEN EXCLUDED.image_url IS NOT NULL AND EXCLUDED.image_url <> ''
			THEN EXCLUDED.image_url ELSE news.image_url END,
		long_description = CASE WHEN EXCLUDED.long_description IS NOT NULL AND EXCLUDED.long_description <> ''
			AND length(EXCLUDED.long_description) > length(news.long_description)
			THEN EXCLUDED.long_description ELSE news.long_description END,
		category = CASE WHEN EXCLUDED.category IS NOT NULL AND EXCLUDED.category <> ''
			THEN EXCLUDED.category ELSE news.category END,
		short_description = CASE WHEN EXCLUDED.short_description IS NOT NULL AND EXCLUDED.short_description <> ''
			THEN EXCLUDED.short_description ELSE news.short_description END,
		fetched_at = EXCLUDED.fetched_at
		`, item.Title, item.Category, item.ShortDescription, item.LongDescription, item.Link,
			item.ImageURL, item.PublishedAt.UTC(), item.FetchedAt.UTC())
	}

	br := s.db.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("%s: batch item %d: %w", op, i, err)
		}
	}

	return nil
}

// ListNews возвращает страницу новостей с курсорной пагинацией.
// Сортировка фиксирована: published_at DESC, id DESC.
// page_token — непрозрачная строка (base64url).
// При некорректном токене возвращает storage.ErrInvalidCursor.
func (s *Storage) ListNews(ctx context.Context, opts models.ListOptions) (*models.Page, error) {
	const op = "storage.postgres.ListNews"

	limit := opts.Limit
	if limit <= 0 {
		// Защита от нуля/отрицательного значения.
		limit = 1
	}

	var rows pgx.Rows
	var err error

	if opts.PageToken == "" {
		rows, err = s.db.Query(ctx, `
		SELECT id, title, category, short_description, long_description, link, image_url, published_at, fetched_at
		FROM news 
		ORDER BY published_at DESC, id DESC 
		LIMIT $1
		`, limit)
	} else {
		pubCur, idCur, decErr := decodePageToken(opts.PageToken)
		if decErr != nil {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrInvalidCursor)
		}

		rows, err = s.db.Query(ctx, `
		SELECT id, title, category, short_description, long_description, link, image_url, published_at, fetched_at
		FROM news 
		WHERE (published_at, id) < ($1, $2)
		ORDER BY published_at DESC, id DESC
		LIMIT $3
		`, pubCur, idCur, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var page models.Page
	for rows.Next() {
		var news models.News
		if scanErr := rows.Scan(
			&news.ID,
			&news.Title,
			&news.Category,
			&news.ShortDescription,
			&news.LongDescription,
			&news.Link,
			&news.ImageURL,
			&news.PublishedAt,
			&news.FetchedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("%s: scan row: %w", op, scanErr)
		}

		// Нормализация в UTC.
		news.PublishedAt = news.PublishedAt.UTC()
		news.FetchedAt = news.FetchedAt.UTC()

		page.Items = append(page.Items, news)
	}

	if rows.Err() != nil {
		return nil, fmt.Errorf("%s: rows: %w", op, rows.Err())
	}

	// Курсор следующей страницы — по последнему элементу.
	if l := len(page.Items); l > 0 {
		last := page.Items[l-1]
		page.NextPageToken = encodePageToken(last.PublishedAt, last.ID)
	} else {
		page.NextPageToken = ""
	}

	return &page, nil
}

// NewsByID возвращает новость по идентификатору.
// Если запись не найдена — storage.ErrNotFound.
// Некорректный формат id трактуется как «нет такой записи».
func (s *Storage) NewsByID(ctx context.Context, id string) (*models.News, error) {
	const op = "storage.postgres.NewsByID"

	correctID, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
	}

	var news models.News
	err = s.db.QueryRow(ctx, `
	SELECT id, title, category, short_description, long_description, link, image_url, published_at, fetched_at
	FROM news 
	WHERE id = $1
	`, correctID).Scan(
		&news.ID,
		&news.Title,
		&news.Category,
		&news.ShortDescription,
		&news.LongDescription,
		&news.Link,
		&news.ImageURL,
		&news.PublishedAt,
		&news.FetchedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	news.PublishedAt = news.PublishedAt.UTC()
	news.FetchedAt = news.FetchedAt.UTC()

	return &news, nil
}

// encodePageToken кодирует пару ключей страницы в непрозрачный токен для клиента.
func encodePageToken(publishedAt time.Time, id uuid.UUID) string {
	raw := fmt.Sprintf("%d|%s", publishedAt.UTC().UnixNano(), id.String())

	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodePageToken декодирует токен обратно в пару ключей.
func decodePageToken(token string) (time.Time, uuid.UUID, error) {
	res, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}

	parts := strings.SplitN(string(res), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("bad parts")
	}

	t, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}

	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}

	return time.Unix(0, t).UTC(), id, nil
}
