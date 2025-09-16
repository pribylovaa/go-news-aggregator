package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/news-service/mocks"
	"github.com/stretchr/testify/require"
)

// stubParser — минимальный Parser для тестов fetcher.go.
type stubParser struct {
	mu     sync.Mutex
	gotURL []string
	res    []ParseResult
}

func (s *stubParser) ParseMany(ctx context.Context, urls []string) <-chan ParseResult {
	s.mu.Lock()
	s.gotURL = append([]string(nil), urls...)
	s.mu.Unlock()

	ch := make(chan ParseResult)
	go func() {
		defer close(ch)
		for _, r := range s.res {
			select {
			case <-ctx.Done():
				return
			case ch <- r:
			}
		}
	}()
	return ch
}

func (s *stubParser) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.gotURL...)
}

// newServiceWithFetcherConfig — фабрика сервиса с заданной fetcher-конфигурацией.
func newServiceWithFetcherConfig(t *testing.T, st *mocks.MockStorage, sources []string, interval time.Duration) *Service {
	t.Helper()
	cfg := config.Config{
		Fetcher: config.FetcherConfig{
			Sources:  sources,
			Interval: interval,
		},
	}
	return New(st, cfg)
}

// within проверяет, что момент времени t попал в [from, to].
func within(t time.Time, from, to time.Time) bool {
	return (t.Equal(from) || t.After(from)) && (t.Equal(to) || t.Before(to))
}

// TestIngestOnce_EmptyBatch_SkipsSave — все элементы отфильтрованы finalizeNews -> SaveNews не зовётся.
func TestIngestOnce_EmptyBatch_SkipsSave(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	st := mocks.NewMockStorage(ctrl)

	parser := &stubParser{
		res: []ParseResult{
			{
				URL: "u1",
				Items: []models.News{
					{Title: "", Link: "https://a"},
					{Title: "ok", Link: ""},
					{Title: "   ", Link: "https://spaces"},
				},
			},
			{
				URL:   "u2",
				Items: nil,
			},
		},
	}

	svc := newServiceWithFetcherConfig(t, st, []string{"u1", "u2"}, time.Hour)

	err := svc.ingestOnce(context.Background(), parser, []string{"u1", "u2"})
	require.NoError(t, err)
}

// TestIngestOnce_SaveBatch_OK — happy-path: корректные элементы доводятся и сохраняются пачкой.
func TestIngestOnce_SaveBatch_OK(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	st := mocks.NewMockStorage(ctrl)

	// «Сырые» элементы:
	// 1) long пустой -> должен упасть в short; дата 0 -> подменится nowUTC.
	// 2) валидный с PublishedAt в зоне +03:00 -> должен стать UTC.
	raw1 := models.News{
		Title:            "  A  ",
		Link:             "https://example.org/a",
		ShortDescription: " short ",
		LongDescription:  "   ",
		PublishedAt:      time.Time{},
	}
	raw2 := models.News{
		Title:       "B",
		Link:        "https://example.org/b",
		PublishedAt: time.Date(2024, 12, 31, 23, 59, 0, 0, time.FixedZone("MSK", 3*3600)),
	}
	parser := &stubParser{
		res: []ParseResult{
			{URL: "u1", Items: []models.News{raw1}},
			{URL: "u2", Items: []models.News{raw2, {Title: "", Link: "drop-me"}}},
		},
	}

	var saved []models.News
	st.EXPECT().
		SaveNews(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []models.News) error {
			// Сохранение копии для последующих проверок.
			saved = append([]models.News(nil), items...)
			return nil
		})

	svc := newServiceWithFetcherConfig(t, st, []string{"u1", "u2"}, time.Hour)

	before := time.Now().UTC()
	err := svc.ingestOnce(context.Background(), parser, []string{"u1", "u2"})
	after := time.Now().UTC()
	require.NoError(t, err)

	require.Len(t, saved, 2, "ожидали два валидных элемента в батче")

	for _, it := range saved {
		require.False(t, it.FetchedAt.IsZero(), "FetchedAt должен быть установлен")
		require.True(t, within(it.FetchedAt, before, after), "FetchedAt должен быть внутри вызова ingestOnce")

		require.False(t, it.PublishedAt.IsZero(), "PublishedAt не должен быть нулевым")
		require.True(t, it.PublishedAt.Location() == time.UTC, "PublishedAt должен быть в UTC")

		if it.Link == raw1.Link {
			require.Equal(t, "short", it.ShortDescription)
			require.Equal(t, "short", it.LongDescription)
			require.Equal(t, "A", it.Title)
		}

		if it.Link == raw2.Link {
			require.Equal(t, "B", it.Title)
		}
	}
}

// TestIngestOnce_ParserError_Continues — ошибка одной ленты не мешает собрать другую.
func TestIngestOnce_ParserError_Continues(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	st := mocks.NewMockStorage(ctrl)

	parser := &stubParser{
		res: []ParseResult{
			{URL: "bad", Err: errors.New("boom")},
			{URL: "ok", Items: []models.News{{Title: "T", Link: "https://ok"}}},
		},
	}

	st.EXPECT().
		SaveNews(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []models.News) error {
			require.Len(t, items, 1)
			require.Equal(t, "https://ok", items[0].Link)
			require.Equal(t, "T", items[0].Title)
			return nil
		})

	svc := newServiceWithFetcherConfig(t, st, []string{"bad", "ok"}, time.Hour)

	require.NoError(t, svc.ingestOnce(context.Background(), parser, []string{"bad", "ok"}))
}

// TestIngestOnce_SaveError_Propagates — ошибка SaveNews должна подняться наверх.
func TestIngestOnce_SaveError_Propagates(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	st := mocks.NewMockStorage(ctrl)

	parser := &stubParser{
		res: []ParseResult{
			{URL: "u", Items: []models.News{{Title: "T", Link: "https://u"}}},
		},
	}

	st.EXPECT().
		SaveNews(gomock.Any(), gomock.Any()).
		Return(errors.New("db down"))

	svc := newServiceWithFetcherConfig(t, st, []string{"u"}, time.Hour)

	err := svc.ingestOnce(context.Background(), parser, []string{"u"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "save_news")
}

// TestStartIngest_NoSources_ReturnsError — если источников нет, возвращается ошибка.
func TestStartIngest_NoSources_ReturnsError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	st := mocks.NewMockStorage(ctrl)

	svc := newServiceWithFetcherConfig(t, st, nil, time.Minute)

	parser := &stubParser{}
	err := svc.StartIngest(context.Background(), parser)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no sources configured")
}

// TestStartIngest_OneShotAndCancel — стартуем, выполняем первый проход и корректно останавливаемся по ctx.
func TestStartIngest_OneShotAndCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	st := mocks.NewMockStorage(ctrl)

	sources := []string{"https://example.org/rss.xml"}

	parser := &stubParser{
		res: []ParseResult{
			{URL: sources[0], Items: []models.News{{Title: "T", Link: "https://x"}}},
		},
	}

	savedCh := make(chan struct{}, 1)

	st.EXPECT().
		SaveNews(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, items []models.News) error {
			require.Len(t, items, 1)
			require.Equal(t, "https://x", items[0].Link)
			select {
			case savedCh <- struct{}{}:
			default:
			}
			return nil
		})

	svc := newServiceWithFetcherConfig(t, st, sources, 24*time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- svc.StartIngest(ctx, parser) }()

	select {
	case <-savedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first ingest tick")
	}

	require.ElementsMatch(t, sources, parser.got())

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for StartIngest to return")
	}
}
