package service

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/news-service/mocks"
	"github.com/stretchr/testify/require"
)

// Файл unit-тестов для сервисного слоя (queries.go).
//
// Покрываем ключевую бизнес-логику:
//  - ListNews:
//      * нормализация лимита (limit<=0 → default; limit>max → max);
//      * сохранение page_token при проксировании в стораж;
//      * маппинг storage.ErrInvalidCursor → service.ErrInvalidCursor;
//      * прозрачная прокидка «остальных» ошибок стораджа;
//      * happy-path (возврат страницы как есть).
//  - NewsByID:
//      * маппинг storage.ErrNotFound → service.ErrNotFound;
//      * прозрачная прокидка «остальных» ошибок;
//      * happy-path (возврат сущности как есть).

// newSvcForTest — фабрика Service с контролируемым cfg и мок-хранилищем.
func newSvcForTest(t *testing.T, st storage.Storage) *Service {
	t.Helper()
	cfg := config.Config{
		LimitsConfig: config.LimitsConfig{
			Default: 12,
			Max:     100,
		},
	}

	return New(st, cfg)
}

// TestListNews_NormalizesLimit_Default — limit <= 0 -> cfg.LimitsConfig.Default.
func TestListNews_NormalizesLimit_Default(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSt := mocks.NewMockStorage(ctrl)

	// Ожидаем два последовательных вызова ListNews:
	gomock.InOrder(
		mockSt.EXPECT().
			ListNews(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, opts models.ListOptions) (*models.Page, error) {
				require.Equal(t, int32(12), opts.Limit, "limit must normalize to default on zero")
				require.Equal(t, "", opts.PageToken, "page_token must pass through (empty here)")
				return &models.Page{}, nil
			}),
		mockSt.EXPECT().
			ListNews(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, opts models.ListOptions) (*models.Page, error) {
				require.Equal(t, int32(12), opts.Limit, "limit must normalize to default on negative")
				require.Equal(t, "", opts.PageToken, "page_token must pass through (empty here)")
				return &models.Page{}, nil
			}),
	)

	svc := newSvcForTest(t, mockSt)

	// limit == 0 -> default.
	_, err := svc.ListNews(context.Background(), models.ListOptions{Limit: 0})
	require.NoError(t, err)

	// limit < 0 -> default.
	_, err = svc.ListNews(context.Background(), models.ListOptions{Limit: -5})
	require.NoError(t, err)

}

// TestListNews_NormalizesLimit_MaxCap — limit > max -> cfg.LimitsConfig.Max.
func TestListNews_NormalizesLimit_MaxCap(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSt := mocks.NewMockStorage(ctrl)

	var captured models.ListOptions
	mockSt.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts models.ListOptions) (*models.Page, error) {
			captured = opts
			return &models.Page{}, nil
		})

	svc := newSvcForTest(t, mockSt)

	_, err := svc.ListNews(context.Background(), models.ListOptions{Limit: 1000})
	require.NoError(t, err)
	require.Equal(t, int32(100), captured.Limit)
}

// TestListNews_PreservesPageToken — сервис должен прокинуть page_token в стораж как есть.
func TestListNews_PreservesPageToken(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSt := mocks.NewMockStorage(ctrl)

	wantToken := "opaque-cursor-token"
	var captured models.ListOptions
	mockSt.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts models.ListOptions) (*models.Page, error) {
			captured = opts
			return &models.Page{}, nil
		})

	svc := newSvcForTest(t, mockSt)

	_, err := svc.ListNews(context.Background(), models.ListOptions{Limit: 10, PageToken: wantToken})
	require.NoError(t, err)
	require.Equal(t, wantToken, captured.PageToken)
}

// TestListNews_InvalidCursor_Mapped — storage.ErrInvalidCursor -> ErrInvalidCursor сервиса.
func TestListNews_InvalidCursor_Mapped(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSt := mocks.NewMockStorage(ctrl)
	mockSt.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		Return(nil, storage.ErrInvalidCursor)

	svc := newSvcForTest(t, mockSt)

	_, err := svc.ListNews(context.Background(), models.ListOptions{Limit: 10, PageToken: "bad"})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidCursor)
}

// TestListNews_StorageError_Propagated — иные ошибки стораджа.
func TestListNews_StorageError_Propagated(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSt := mocks.NewMockStorage(ctrl)
	mockSt.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("db fail"))

	svc := newSvcForTest(t, mockSt)

	_, err := svc.ListNews(context.Background(), models.ListOptions{Limit: 10})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalidCursor)
}

// TestListNews_OK — happy-path: возвращаемая страница пробрасывается без изменений.
func TestListNews_OK(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	page := &models.Page{
		Items:         []models.News{{Title: "a"}, {Title: "b"}},
		NextPageToken: "next",
	}

	mockSt := mocks.NewMockStorage(ctrl)
	mockSt.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		Return(page, nil)

	svc := newSvcForTest(t, mockSt)

	got, err := svc.ListNews(context.Background(), models.ListOptions{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, page, got)
}

// TestNewsByID_NotFound_Mapped — storage.ErrNotFound -> ErrNotFound сервиса.
func TestNewsByID_NotFound_Mapped(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSt := mocks.NewMockStorage(ctrl)
	mockSt.EXPECT().
		NewsByID(gomock.Any(), "id-404").
		Return(nil, storage.ErrNotFound)

	svc := newSvcForTest(t, mockSt)

	_, err := svc.NewsByID(context.Background(), "id-404")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNotFound)
}

// TestNewsByID_StorageError_Propagated — иные ошибки стораджа.
func TestNewsByID_StorageError_Propagated(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockSt := mocks.NewMockStorage(ctrl)
	mockSt.EXPECT().
		NewsByID(gomock.Any(), "id-err").
		Return(nil, errors.New("db fail"))

	svc := newSvcForTest(t, mockSt)

	_, err := svc.NewsByID(context.Background(), "id-err")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotFound)
}

// TestNewsByID_OK — happy-path: сущность пробрасывается без изменений.
func TestNewsByID_OK(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	entity := &models.News{Title: "ok"}

	mockSt := mocks.NewMockStorage(ctrl)
	mockSt.EXPECT().
		NewsByID(gomock.Any(), "id-ok").
		Return(entity, nil)

	svc := newSvcForTest(t, mockSt)

	got, err := svc.NewsByID(context.Background(), "id-ok")
	require.NoError(t, err)
	require.Equal(t, entity, got)
}
