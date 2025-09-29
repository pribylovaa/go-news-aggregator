package grpc

// Тесты транспортного слоя (gRPC) для CommentsService.
// Подход как в users-service:
//  - используем gomock для слоя storage ниже сервиса;
//  - конструируем реальный service.Service поверх моков;
//  - проверяем валидацию входов (UUID/пустые строки), маппинг ошибок сервиса -> gRPC codes,
//    и конвертацию доменной модели в protobuf (включая таймстемпы).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	commentsv1 "github.com/pribylovaa/go-news-aggregator/comments-service/gen/go/comments"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/service"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/comments-service/mocks"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newServerWithMocks — хелпер сборки CommentsServer с реальным сервисом поверх мок-хранилища.
func newServerWithMocks(t *testing.T) (*CommentsServer, *mocks.MockStorage, *gomock.Controller) {
	t.Helper()

	ctrl := gomock.NewController(t)
	ms := mocks.NewMockStorage(ctrl)

	// Предполагаем конструктор сервиса аналогично users-service: New(storage, cfg).
	svc := service.New(ms, config.Config{})
	srv := NewCommentsServer(svc)

	return srv, ms, ctrl
}

// mustComment — быстрый хелпер доменной модели (с детерминированными таймстемпами).
func mustComment(newsID uuid.UUID, parentID, username, content string) *models.Comment {
	ts := time.Unix(1710000000, 0).UTC()
	return &models.Comment{
		ID:           uuid.New().String(),
		NewsID:       newsID,
		ParentID:     parentID,
		UserID:       uuid.New(),
		Username:     username,
		Content:      content,
		Level:        0,
		RepliesCount: 0,
		IsDeleted:    false,
		CreatedAt:    ts,
		UpdatedAt:    ts.Add(time.Minute),
		ExpiresAt:    ts.Add(24 * time.Hour),
	}
}

// Невалидные UUID на уровне транспорта: user_id и (для корня) news_id.
func TestGRPC_CreateComment_InvalidUUIDs(t *testing.T) {
	srv, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	// Неверный user_id
	_, err := srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId: "not-a-uuid",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// Корень: пустой parent_id, но news_id не UUID
	_, err = srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId:   uuid.New().String(),
		NewsId:   "bad-news",
		ParentId: "",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// Валидация/ошибки сервиса: ErrInvalidArgument от сервиса транслируется в InvalidArgument.
func TestGRPC_CreateComment_ServiceValidation_InvalidArgument(t *testing.T) {
	srv, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	// Пустой username (после TrimSpace) -> сервис вернёт ErrInvalidArgument,
	// storage не вызывается.
	uid := uuid.New()
	nid := uuid.New()

	_, err := srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId:   uid.String(),
		NewsId:   nid.String(),
		Username: "   ",
		Content:  "ok",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// Ошибки стораджа -> сервисные -> gRPC-коды.
func TestGRPC_CreateComment_ErrorMapping(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()

	// ParentNotFound -> NotFound
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.AssignableToTypeOf(models.Comment{})).
		Return(nil, storage.ErrParentNotFound)
	_, err := srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId:   uid.String(),
		ParentId: "507f1f77bcf86cd799439011",
		Username: "u",
		Content:  "c",
	})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))

	// ThreadExpired -> FailedPrecondition
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.AssignableToTypeOf(models.Comment{})).
		Return(nil, storage.ErrThreadExpired)
	_, err = srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId:   uid.String(),
		ParentId: "507f1f77bcf86cd799439012",
		Username: "u",
		Content:  "c",
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	// MaxDepthExceeded -> FailedPrecondition
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.AssignableToTypeOf(models.Comment{})).
		Return(nil, storage.ErrMaxDepthExceeded)
	_, err = srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId:   uid.String(),
		ParentId: "507f1f77bcf86cd799439013",
		Username: "u",
		Content:  "c",
	})
	require.Error(t, err)
	require.Equal(t, codes.FailedPrecondition, status.Code(err))

	// Conflict -> AlreadyExists
	nid := uuid.New()
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.AssignableToTypeOf(models.Comment{})).
		Return(nil, storage.ErrConflict)
	_, err = srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId:   uid.String(),
		NewsId:   nid.String(),
		Username: "u",
		Content:  "c",
	})
	require.Error(t, err)
	require.Equal(t, codes.AlreadyExists, status.Code(err))

	// Произвольная внутренняя ошибка -> Internal
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.AssignableToTypeOf(models.Comment{})).
		Return(nil, errors.New("db down"))
	_, err = srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId:   uid.String(),
		NewsId:   nid.String(),
		Username: "u",
		Content:  "c",
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// Успешное создание комментария (корень): проверяем маппинг полей и таймстемпов.
func TestGRPC_CreateComment_OK(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	uid := uuid.New()
	nid := uuid.New()
	want := mustComment(nid, "", "alice", "hello")
	want.UserID = uid

	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.AssignableToTypeOf(models.Comment{})).
		DoAndReturn(func(_ context.Context, c models.Comment) (*models.Comment, error) {
			require.Equal(t, uid, c.UserID)
			require.Equal(t, nid, c.NewsID)
			require.Equal(t, "", c.ParentID)
			require.Equal(t, "alice", c.Username) // TrimSpace произошёл в сервисе
			require.Equal(t, "hello", c.Content)  // TrimSpace произошёл в сервисе
			return want, nil
		})

	resp, err := srv.CreateComment(context.Background(), &commentsv1.CreateCommentRequest{
		UserId:   uid.String(),
		NewsId:   nid.String(),
		Username: "  alice  ",
		Content:  "  hello  ",
	})
	require.NoError(t, err)

	c := resp.GetComment()
	require.NotNil(t, c, "response.Comment must be set")

	require.Equal(t, want.ID, c.GetId())
	require.Equal(t, nid.String(), c.GetNewsId())
	require.Equal(t, "", c.GetParentId())
	require.Equal(t, uid.String(), c.GetUserId())
	require.Equal(t, "alice", c.GetUsername())
	require.Equal(t, "hello", c.GetContent())
	require.EqualValues(t, want.Level, c.GetLevel())
	require.EqualValues(t, want.RepliesCount, c.GetRepliesCount())
	require.Equal(t, want.IsDeleted, c.GetIsDeleted())
	require.Equal(t, want.CreatedAt.Unix(), c.GetCreatedAt())
	require.Equal(t, want.UpdatedAt.Unix(), c.GetUpdatedAt())
	require.Equal(t, want.ExpiresAt.Unix(), c.GetExpiresAt())
}

// Пустой id -> InvalidArgument (на уровне сервиса).
func TestGRPC_DeleteComment_InvalidArgument(t *testing.T) {
	srv, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.DeleteComment(context.Background(), &commentsv1.DeleteCommentRequest{Id: "   "})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// NotFound/Internal маппинг.
func TestGRPC_DeleteComment_ErrorMapping(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	// NotFound
	ms.EXPECT().DeleteComment(gomock.Any(), "42").Return(storage.ErrNotFound)
	_, err := srv.DeleteComment(context.Background(), &commentsv1.DeleteCommentRequest{Id: "42"})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))

	// Internal
	ms.EXPECT().DeleteComment(gomock.Any(), "42").Return(errors.New("db down"))
	_, err = srv.DeleteComment(context.Background(), &commentsv1.DeleteCommentRequest{Id: "42"})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// OK.
func TestGRPC_DeleteComment_OK(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	ms.EXPECT().DeleteComment(gomock.Any(), "55").Return(nil)
	_, err := srv.DeleteComment(context.Background(), &commentsv1.DeleteCommentRequest{Id: "55"})
	require.NoError(t, err)
}

// Пустой id валидируется на уровне транспорта.
func TestGRPC_CommentByID_EmptyID(t *testing.T) {
	srv, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.CommentByID(context.Background(), &commentsv1.CommentByIDRequest{Id: "  "})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGRPC_CommentByID_ErrorMapping(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	// NotFound
	ms.EXPECT().CommentByID(gomock.Any(), "77").Return(nil, storage.ErrNotFound)
	_, err := srv.CommentByID(context.Background(), &commentsv1.CommentByIDRequest{Id: "77"})
	require.Error(t, err)
	require.Equal(t, codes.NotFound, status.Code(err))

	// Internal
	ms.EXPECT().CommentByID(gomock.Any(), "77").Return(nil, errors.New("db down"))
	_, err = srv.CommentByID(context.Background(), &commentsv1.CommentByIDRequest{Id: "77"})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGRPC_CommentByID_OK(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	want := mustComment(uuid.New(), "", "bob", "hi")
	ms.EXPECT().CommentByID(gomock.Any(), want.ID).Return(want, nil)

	resp, err := srv.CommentByID(context.Background(), &commentsv1.CommentByIDRequest{Id: want.ID})
	require.NoError(t, err)

	c := resp.GetComment()
	require.NotNil(t, c, "response.Comment must be set")

	require.Equal(t, want.ID, c.GetId())
	require.Equal(t, want.NewsID.String(), c.GetNewsId())
	require.Equal(t, want.ParentID, c.GetParentId())
	require.Equal(t, want.UserID.String(), c.GetUserId())
	require.Equal(t, want.Username, c.GetUsername())
	require.Equal(t, want.Content, c.GetContent())
	require.EqualValues(t, want.Level, c.GetLevel())
	require.EqualValues(t, want.RepliesCount, c.GetRepliesCount())
	require.Equal(t, want.IsDeleted, c.GetIsDeleted())
	require.Equal(t, want.CreatedAt.Unix(), c.GetCreatedAt())
	require.Equal(t, want.UpdatedAt.Unix(), c.GetUpdatedAt())
	require.Equal(t, want.ExpiresAt.Unix(), c.GetExpiresAt())
}

// Неверный UUID news_id валидируется на уровне транспорта.
func TestGRPC_ListByNews_InvalidNewsID(t *testing.T) {
	srv, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.ListByNews(context.Background(), &commentsv1.ListByNewsRequest{
		NewsId: "not-uuid",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// InvalidCursor/Internal маппинг.
func TestGRPC_ListByNews_ErrorMapping(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	nid := uuid.New()
	ms.EXPECT().
		ListByNews(gomock.Any(), nid.String(), gomock.Any()).
		Return(nil, storage.ErrInvalidCursor)
	_, err := srv.ListByNews(context.Background(), &commentsv1.ListByNewsRequest{
		NewsId: nid.String(), PageToken: "bad",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	ms.EXPECT().
		ListByNews(gomock.Any(), nid.String(), gomock.Any()).
		Return(nil, errors.New("db down"))
	_, err = srv.ListByNews(context.Background(), &commentsv1.ListByNewsRequest{
		NewsId: nid.String(),
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// OK.
func TestGRPC_ListByNews_OK(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	nid := uuid.New()
	a := mustComment(nid, "", "a", "x")
	b := mustComment(nid, "", "b", "y")

	ms.EXPECT().
		ListByNews(gomock.Any(), nid.String(), gomock.Any()).
		Return(&models.Page{
			Items:         []models.Comment{*a, *b},
			NextPageToken: "t2",
		}, nil)

	got, err := srv.ListByNews(context.Background(), &commentsv1.ListByNewsRequest{
		NewsId:    nid.String(),
		PageSize:  25,
		PageToken: "t1",
	})
	require.NoError(t, err)
	require.Len(t, got.GetComments(), 2)
	require.Equal(t, "t2", got.GetNextPageToken())

	g0, g1 := got.GetComments()[0], got.GetComments()[1]
	require.Equal(t, a.ID, g0.GetId())
	require.Equal(t, b.ID, g1.GetId())
	require.Equal(t, a.CreatedAt.Unix(), g0.GetCreatedAt())
	require.Equal(t, b.CreatedAt.Unix(), g1.GetCreatedAt())
}

// Пустой parent_id валидируется на уровне транспорта.
func TestGRPC_ListReplies_EmptyParentID(t *testing.T) {
	srv, _, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	_, err := srv.ListReplies(context.Background(), &commentsv1.ListRepliesRequest{
		ParentId: "   ",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// InvalidCursor/Internal маппинг.
func TestGRPC_ListReplies_ErrorMapping(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	pid := "507f1f77bcf86cd799439011"

	ms.EXPECT().
		ListReplies(gomock.Any(), pid, gomock.Any()).
		Return(nil, storage.ErrInvalidCursor)
	_, err := srv.ListReplies(context.Background(), &commentsv1.ListRepliesRequest{
		ParentId:  pid,
		PageToken: "bad",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	ms.EXPECT().
		ListReplies(gomock.Any(), pid, gomock.Any()).
		Return(nil, errors.New("db down"))
	_, err = srv.ListReplies(context.Background(), &commentsv1.ListRepliesRequest{
		ParentId: pid,
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// OK.
func TestGRPC_ListReplies_OK(t *testing.T) {
	srv, ms, ctrl := newServerWithMocks(t)
	defer ctrl.Finish()

	nid := uuid.New()
	pid := "507f1f77bcf86cd799439011"
	a := mustComment(nid, pid, "a", "x")
	b := mustComment(nid, pid, "b", "y")

	ms.EXPECT().
		ListReplies(gomock.Any(), pid, gomock.Any()).
		Return(&models.Page{
			Items:         []models.Comment{*a, *b},
			NextPageToken: "t2",
		}, nil)

	got, err := srv.ListReplies(context.Background(), &commentsv1.ListRepliesRequest{
		ParentId:  pid,
		PageSize:  50,
		PageToken: "t1",
	})
	require.NoError(t, err)
	require.Len(t, got.GetComments(), 2)
	require.Equal(t, "t2", got.GetNextPageToken())

	g0, g1 := got.GetComments()[0], got.GetComments()[1]
	require.Equal(t, a.ParentID, g0.GetParentId())
	require.Equal(t, b.ParentID, g1.GetParentId())
	require.Equal(t, a.ExpiresAt.Unix(), g0.GetExpiresAt())
	require.Equal(t, b.ExpiresAt.Unix(), g1.GetExpiresAt())
}
