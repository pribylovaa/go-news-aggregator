package postgres

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Интеграционные тесты для пакета postgres (реализация хранилища в news.go):
// — поднимают реальный PostgreSQL через testcontainers-go (образ postgres:16-alpine);
// — применяют миграции из ./migrations;
// — проверяют:
//    SaveNews: insert и upsert по link с политикой «не затирать пустыми/короче»;
//    ListNews: keyset-пагинация (page_token), limit<=0 → 1, тай-брейк по (published_at DESC, id DESC);
//    NewsByID: успешный сценарий и ErrNotFound при невалидном UUID;
//    обработку некорректного page_token (не-base64, нет разделителя, плохие timestamp/UUID);
//    encode/decode page_token (round-trip).

// Запуск локально:
//   GO_TEST_INTEGRATION=1 go test ./internal/storage/postgres -v -race -count=1

// repoRootFromThisFile — определяет корень репозитория относительно текущего файла тестов.
// Используется для поиска SQL-миграций в каталоге ./migrations независимо от текущего рабочего каталога.
func repoRootFromThisFile() string {
	// internal/storage/postgres/... -> подняться на 3 уровня до корня.
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// readMigration — читает содержимое SQL-миграции из подкаталога ./migrations.
func readMigration(t *testing.T, name string) string {
	t.Helper()
	root := repoRootFromThisFile()
	path := filepath.Join(root, "migrations", name)
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read migration %s", path)
	return string(b)
}

// startPostgres — поднимает PostgreSQL через testcontainers-go,
// применяет миграции news и возвращает инициализированное хранилище и функцию очистки.
// Если переменная окружения GO_TEST_INTEGRATION не установлена — тест пропускается.
func startPostgres(t *testing.T) (*Storage, func()) {
	t.Helper()
	if os.Getenv("GO_TEST_INTEGRATION") == "" {
		t.Skip("integration tests are disabled (set GO_TEST_INTEGRATION=1)")
	}

	ctx := context.Background()
	req := tc.ContainerRequest{
		Image:        "postgres:16-alpine",
		Env:          map[string]string{"POSTGRES_USER": "user", "POSTGRES_PASSWORD": "pass", "POSTGRES_DB": "db"},
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	c, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)

	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5432/tcp")
	dsn := fmt.Sprintf("postgres://user:pass@%s:%s/db?sslmode=disable", host, port.Port())

	// применяем миграции.
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, readMigration(t, "1_init_news.up.sql"))
	require.NoError(t, err)

	st, err := New(ctx, dsn)
	require.NoError(t, err)

	cleanup := func() {
		st.Close()
		_ = c.Terminate(context.Background())
	}
	return st, cleanup
}

func TestIntegration_SaveNews_Upsert_And_ByID_OK(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)

	item1 := models.News{
		Title:            "Title v1",
		Category:         "",
		ShortDescription: "",
		LongDescription:  "short",
		Link:             "https://example.org/a",
		ImageURL:         "",
		PublishedAt:      now.Add(-time.Hour),
		FetchedAt:        now,
	}
	require.NoError(t, st.SaveNews(context.Background(), []models.News{item1}))

	page1, err := st.ListNews(context.Background(), models.ListOptions{Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, page1.Items)

	var inserted models.News
	for _, it := range page1.Items {
		if it.Link == item1.Link {
			inserted = it
			break
		}
	}
	require.NotEqual(t, uuid.Nil, inserted.ID, "inserted item not found by link")

	item2 := models.News{
		Title:            "Title v2",
		Category:         "life",
		ShortDescription: "teaser",
		LongDescription:  "this is a much much longer text than v1",
		Link:             item1.Link, // тот же link
		ImageURL:         "https://cdn.example.org/img.jpg",
		PublishedAt:      now.Add(-2 * time.Hour), // не должно поменяться
		FetchedAt:        now.Add(time.Minute),    // обновится
	}
	require.NoError(t, st.SaveNews(context.Background(), []models.News{item2}))

	got, err := st.NewsByID(context.Background(), inserted.ID.String())
	require.NoError(t, err)

	require.Equal(t, "Title v2", got.Title)
	require.Equal(t, "life", got.Category)
	require.Equal(t, "teaser", got.ShortDescription)
	require.Equal(t, "https://cdn.example.org/img.jpg", got.ImageURL)
	require.Equal(t, item1.PublishedAt, got.PublishedAt, "published_at must not change on upsert")
	require.GreaterOrEqual(t, got.FetchedAt.Unix(), item1.FetchedAt.Unix())
	require.Equal(t, item2.LongDescription, got.LongDescription)
}

func TestIntegration_SaveNews_NoOverwriteOnEmptyOrShorter(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	now := time.Now().UTC().Truncate(time.Second)

	orig := models.News{
		Title:            "v1",
		Category:         "world",
		ShortDescription: "short",
		LongDescription:  "this is a very long original text",
		Link:             "https://example.org/no-overwrite",
		ImageURL:         "https://cdn.example.org/a.jpg",
		PublishedAt:      now.Add(-time.Hour),
		FetchedAt:        now,
	}
	require.NoError(t, st.SaveNews(context.Background(), []models.News{orig}))

	upd := models.News{
		Title:            "v2",
		Category:         "",
		ShortDescription: "",
		LongDescription:  "shorter",
		Link:             orig.Link,
		ImageURL:         "",
		PublishedAt:      now.Add(-2 * time.Hour),
		FetchedAt:        now.Add(time.Minute),
	}
	require.NoError(t, st.SaveNews(context.Background(), []models.News{upd}))

	page, err := st.ListNews(context.Background(), models.ListOptions{Limit: 50})
	require.NoError(t, err)

	var found *models.News
	for i := range page.Items {
		if page.Items[i].Link == orig.Link {
			found = &page.Items[i]
			break
		}
	}
	require.NotNil(t, found, "inserted item not found in listing")

	require.Equal(t, "v2", found.Title)
	require.Equal(t, "world", found.Category)
	require.Equal(t, "short", found.ShortDescription)
	require.Equal(t, "https://cdn.example.org/a.jpg", found.ImageURL)
	require.Equal(t, orig.LongDescription, found.LongDescription, "long_description must not be overwritten by a shorter text")
	require.Equal(t, orig.PublishedAt, found.PublishedAt, "published_at must not change on upsert")
	require.GreaterOrEqual(t, found.FetchedAt.Unix(), orig.FetchedAt.Unix())
}

func TestIntegration_ListNews_Pagination_OK(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	base := time.Now().UTC().Truncate(time.Second)
	var batch []models.News
	for i := 0; i < 5; i++ {
		batch = append(batch, models.News{
			Title:            fmt.Sprintf("N%d", i),
			Category:         "cat",
			LongDescription:  "x",
			ShortDescription: "y",
			Link:             fmt.Sprintf("https://example.org/%d", i),
			ImageURL:         "",
			PublishedAt:      base.Add(-time.Duration(i) * time.Minute),
			FetchedAt:        base,
		})
	}
	require.NoError(t, st.SaveNews(context.Background(), batch))

	// Первая страница.
	p1, err := st.ListNews(context.Background(), models.ListOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, p1.Items, 2)
	require.True(t, p1.Items[0].PublishedAt.After(p1.Items[1].PublishedAt) || p1.Items[0].PublishedAt.Equal(p1.Items[1].PublishedAt))
	require.NotEmpty(t, p1.NextPageToken)

	// Вторая страница.
	p2, err := st.ListNews(context.Background(), models.ListOptions{Limit: 2, PageToken: p1.NextPageToken})
	require.NoError(t, err)
	require.Len(t, p2.Items, 2)
	require.NotEmpty(t, p2.NextPageToken)
	require.NotEqual(t, p1.Items[1].ID, p2.Items[0].ID)

	// Третья страница (последняя).
	p3, err := st.ListNews(context.Background(), models.ListOptions{Limit: 2, PageToken: p2.NextPageToken})
	require.NoError(t, err)
	require.Len(t, p3.Items, 1)

	// Четвёртая страница — должна быть пустой и без next_token.
	p4, err := st.ListNews(context.Background(), models.ListOptions{Limit: 2, PageToken: p3.NextPageToken})
	require.NoError(t, err)
	require.Empty(t, p4.Items)
	require.Equal(t, "", p4.NextPageToken)
}

func TestIntegration_ListNews_LimitZero_DefaultsToOne(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	base := time.Now().UTC().Truncate(time.Second)
	items := []models.News{
		{
			Title:            "A",
			Category:         "c",
			LongDescription:  "x",
			ShortDescription: "y",
			Link:             "https://example.org/lim0/a",
			PublishedAt:      base.Add(-1 * time.Minute),
			FetchedAt:        base,
		},
		{
			Title:            "B",
			Category:         "c",
			LongDescription:  "x",
			ShortDescription: "y",
			Link:             "https://example.org/lim0/b",
			PublishedAt:      base,
			FetchedAt:        base,
		},
	}
	require.NoError(t, st.SaveNews(context.Background(), items))

	p, err := st.ListNews(context.Background(), models.ListOptions{Limit: 0})
	require.NoError(t, err)
	require.Len(t, p.Items, 1, "limit<=0 must fallback to 1")
	require.NotEmpty(t, p.NextPageToken)
}

func TestIntegration_ListNews_InvalidToken_ReturnsErrInvalidCursor(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.ListNews(context.Background(), models.ListOptions{Limit: 2, PageToken: "%%%not_base64%%%"})
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrInvalidCursor)
}

func TestIntegration_NewsByID_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.NewsByID(context.Background(), uuid.New().String())
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestIntegration_NewsByID_InvalidFormat_IsNotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.NewsByID(context.Background(), "not-a-uuid")
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestIntegration_SaveNews_ContextDeadlineExceeded(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	item := models.News{
		Title:       "deadline",
		Category:    "",
		Link:        "https://example.org/deadline",
		PublishedAt: time.Now().UTC(),
		FetchedAt:   time.Now().UTC(),
	}
	err := st.SaveNews(ctx, []models.News{item})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), context.DeadlineExceeded.Error()))
}

func TestEncodeDecodePageToken_Roundtrip(t *testing.T) {
	pub := time.Date(2024, 7, 1, 12, 0, 0, 123_000_000, time.UTC)
	id := uuid.New()

	token := encodePageToken(pub, id)
	gotPub, gotID, err := decodePageToken(token)
	require.NoError(t, err)
	require.Equal(t, pub, gotPub)
	require.Equal(t, id, gotID)
}

func TestIntegration_ListNews_TieBreakers_PaginateStable(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	pub := time.Now().UTC().Truncate(time.Second)
	var batch []models.News
	for i := 0; i < 3; i++ {
		batch = append(batch, models.News{
			Title:            fmt.Sprintf("T%d", i),
			Category:         "tie",
			LongDescription:  "x",
			ShortDescription: "y",
			Link:             fmt.Sprintf("https://example.org/tie/%d", i),
			PublishedAt:      pub, // одинаковые published_at для всех
			FetchedAt:        pub,
		})
	}
	require.NoError(t, st.SaveNews(context.Background(), batch))

	p1, err := st.ListNews(context.Background(), models.ListOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, p1.Items, 2)
	require.NotEmpty(t, p1.NextPageToken)

	p2, err := st.ListNews(context.Background(), models.ListOptions{Limit: 2, PageToken: p1.NextPageToken})
	require.NoError(t, err)
	require.Len(t, p2.Items, 1)

	seen := map[uuid.UUID]struct{}{}
	for _, it := range append(p1.Items, p2.Items...) {
		seen[it.ID] = struct{}{}
	}
	require.Len(t, seen, 3)
}

func TestDecodePageToken_Errors(t *testing.T) {
	t.Run("not base64", func(t *testing.T) {
		_, _, err := decodePageToken("%%%")
		require.Error(t, err)
	})
	t.Run("no separator", func(t *testing.T) {
		token := base64.RawURLEncoding.EncodeToString([]byte("noseparator"))
		_, _, err := decodePageToken(token)
		require.Error(t, err)
	})
	t.Run("bad timestamp", func(t *testing.T) {
		token := base64.RawURLEncoding.EncodeToString([]byte("not-an-int|" + uuid.New().String()))
		_, _, err := decodePageToken(token)
		require.Error(t, err)
	})
	t.Run("bad uuid", func(t *testing.T) {
		token := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d|bad-uuid", time.Now().UTC().UnixNano())))
		_, _, err := decodePageToken(token)
		require.Error(t, err)
	})
}
