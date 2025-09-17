package grpc

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	newsv1 "github.com/pribylovaa/go-news-aggregator/news-service/gen/go/news"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/service"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/news-service/mocks"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Конфигурация для сервиса в тестах.
func testCfg() config.Config {
	return config.Config{
		LimitsConfig: config.LimitsConfig{
			Default: 12,
			Max:     300,
		},
	}
}

// Фабрика сервисного слоя с gomock-хранилищем.
func newSvcWithMock(t *testing.T) (*service.Service, *mocks.MockStorage, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	st := mocks.NewMockStorage(ctrl)
	return service.New(st, testCfg()), st, ctrl
}

// startGRPC — поднимает bufconn-gRPC-сервер с переданным сервисом
// и возвращает клиент и функцию очистки.
func startGRPC(t *testing.T, svc *service.Service) (newsv1.NewsServiceClient, func()) {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	newsv1.RegisterNewsServiceServer(s, NewNewsServer(svc))

	go func() { _ = s.Serve(lis) }()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }

	cc, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	cleanup := func() { _ = cc.Close(); s.Stop() }
	return newsv1.NewNewsServiceClient(cc), cleanup
}

func TestListNews_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	now := time.Now().UTC().Truncate(time.Second)
	id1, id2 := uuid.New(), uuid.New()

	st.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts models.ListOptions) (*models.Page, error) {
			require.Equal(t, int32(5), opts.Limit)
			require.Equal(t, "cursor", opts.PageToken)

			return &models.Page{
				Items: []models.News{
					{
						ID:               id1,
						Title:            "A",
						Category:         "cat",
						ShortDescription: "s1",
						LongDescription:  "l1",
						Link:             "https://example.org/a",
						ImageURL:         "https://cdn/a.jpg",
						PublishedAt:      now.Add(-time.Hour),
						FetchedAt:        now,
					},
					{
						ID:               id2,
						Title:            "B",
						Category:         "",
						ShortDescription: "s2",
						LongDescription:  "l2",
						Link:             "https://example.org/b",
						ImageURL:         "",
						PublishedAt:      now,
						FetchedAt:        now,
					},
				},
				NextPageToken: "next",
			}, nil
		})

	resp, err := client.ListNews(context.Background(), &newsv1.ListNewsRequest{
		Limit:     5,
		PageToken: "cursor",
	})
	require.NoError(t, err)
	require.Equal(t, "next", resp.NextPageToken)
	require.Len(t, resp.Items, 2)

	got1, got2 := resp.Items[0], resp.Items[1]
	require.Equal(t, id1.String(), got1.Id)
	require.Equal(t, "A", got1.Title)
	require.Equal(t, "cat", got1.Category)
	require.Equal(t, "s1", got1.ShortDescription)
	require.Equal(t, "l1", got1.LongDescription)
	require.Equal(t, "https://example.org/a", got1.Link)
	require.Equal(t, "https://cdn/a.jpg", got1.ImageUrl)
	require.Equal(t, now.Add(-time.Hour).Unix(), got1.PublishedAt)
	require.Equal(t, now.Unix(), got1.FetchedAt)

	require.Equal(t, id2.String(), got2.Id)
	require.Equal(t, "B", got2.Title)
	require.Equal(t, "", got2.ImageUrl)
	require.Equal(t, now.Unix(), got2.PublishedAt)
}

func TestListNews_DefaultLimit_WhenZero(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	// limit=0 -> cfg.Limits.Default (12).
	st.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, opts models.ListOptions) (*models.Page, error) {
			require.Equal(t, int32(12), opts.Limit) // из testCfg()
			require.Equal(t, "", opts.PageToken)
			return &models.Page{Items: nil, NextPageToken: ""}, nil
		})

	resp, err := client.ListNews(context.Background(), &newsv1.ListNewsRequest{
		Limit: 0,
	})
	require.NoError(t, err)
	require.Empty(t, resp.Items)
	require.Empty(t, resp.NextPageToken)
}

func TestListNews_EmptyPage_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	st.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		Return(&models.Page{Items: []models.News{}, NextPageToken: ""}, nil)

	resp, err := client.ListNews(context.Background(), &newsv1.ListNewsRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, resp.Items, 0)
	require.Equal(t, "", resp.NextPageToken)
}

func TestListNews_InvalidCursor_And_Internal(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	// Invalid cursor -> codes.InvalidArgument (storage.ErrInvalidCursor -> service.ErrInvalidCursor).
	st.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		Return(nil, storage.ErrInvalidCursor)

	_, err := client.ListNews(context.Background(), &newsv1.ListNewsRequest{Limit: 10, PageToken: "bad"})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Любая иная ошибка -> codes.Internal.
	st.EXPECT().
		ListNews(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("db down"))

	_, err = client.ListNews(context.Background(), &newsv1.ListNewsRequest{Limit: 10})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestNewsByID_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	now := time.Now().UTC().Truncate(time.Second)
	id := uuid.New()

	st.EXPECT().
		NewsByID(gomock.Any(), id.String()).
		Return(&models.News{
			ID:               id,
			Title:            "T",
			Category:         "world",
			ShortDescription: "s",
			LongDescription:  "l",
			Link:             "https://example.org/t",
			ImageURL:         "https://cdn/t.jpg",
			PublishedAt:      now,
			FetchedAt:        now,
		}, nil)

	resp, err := client.NewsByID(context.Background(), &newsv1.NewsByIDRequest{Id: id.String()})
	require.NoError(t, err)
	require.NotNil(t, resp.Item)

	require.Equal(t, id.String(), resp.Item.Id)
	require.Equal(t, "T", resp.Item.Title)
	require.Equal(t, "world", resp.Item.Category)
	require.Equal(t, "s", resp.Item.ShortDescription)
	require.Equal(t, "l", resp.Item.LongDescription)
	require.Equal(t, "https://example.org/t", resp.Item.Link)
	require.Equal(t, "https://cdn/t.jpg", resp.Item.ImageUrl)
	require.Equal(t, now.Unix(), resp.Item.PublishedAt)
	require.Equal(t, now.Unix(), resp.Item.FetchedAt)
}

func TestNewsByID_NotFound_And_Internal(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	// NotFound -> codes.NotFound.
	st.EXPECT().
		NewsByID(gomock.Any(), "missing").
		Return(nil, storage.ErrNotFound)

	_, err := client.NewsByID(context.Background(), &newsv1.NewsByIDRequest{Id: "missing"})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))

	// Internal -> codes.Internal.
	st.EXPECT().
		NewsByID(gomock.Any(), "boom").
		Return(nil, errors.New("db fail"))

	_, err = client.NewsByID(context.Background(), &newsv1.NewsByIDRequest{Id: "boom"})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestNewsByID_Errors_Table(t *testing.T) {
	t.Parallel()

	type tc struct {
		id       string
		stErr    error
		wantCode codes.Code
	}
	cases := []tc{
		{id: "missing", stErr: storage.ErrNotFound, wantCode: codes.NotFound},
		{id: "boom", stErr: errors.New("db fail"), wantCode: codes.Internal},
	}

	for _, c := range cases {
		svc, st, ctrl := newSvcWithMock(t)
		client, done := startGRPC(t, svc)

		st.EXPECT().
			NewsByID(gomock.Any(), c.id).
			Return(nil, c.stErr)

		_, err := client.NewsByID(context.Background(), &newsv1.NewsByIDRequest{Id: c.id})
		require.Error(t, err)
		require.Equal(t, c.wantCode, status.Code(err))

		done()
		ctrl.Finish()
	}
}
