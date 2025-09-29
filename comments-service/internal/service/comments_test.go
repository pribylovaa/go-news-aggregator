package service

// Тесты сервисного слоя comments-service (internal/service/comments.go).
//
//  Проверяем:
//  - валидацию входов (Create/Delete/Get/List...);
//  - маппинг ошибок storage -> service (InvalidArgument / NotFound / Conflict / ParentNotFound / ThreadExpired / MaxDepthExceeded / InvalidCursor / Internal);
//  - корректность нормализации входных данных (TrimSpace для username/content) и формируемых аргументов вызова storage;
//  - happy-path каждого метода.
//
// Подготовка окружения:
//   # 1) Сгенерировать моки интерфейса хранилища:
//   mockgen -source=./internal/storage/storage.go -destination=./mocks/storage.go -package=mocks
//
//   # 2) Запустить тесты:
//   go test ./internal/service -v -race -count=1
//
// Примечание: моки сгенерированы в пакете /mocks (MockStorage).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/comments-service/mocks"
	"github.com/stretchr/testify/require"
)

// newServiceWithMocks — поднимает сервис с моками стораджа.
func newServiceWithMocks(t *testing.T) (*Service, *mocks.MockStorage, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	ms := mocks.NewMockStorage(ctrl)
	s := &Service{storage: ms}
	return s, ms, ctrl
}

// mustComment — быстрый хелпер для сборки комментария.
func mustComment(newsID uuid.UUID, parentID, username, content string) *models.Comment {
	now := time.Now().UTC()
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
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpiresAt:    now.Add(24 * time.Hour),
	}
}

// Валидация: пустой userID, пустой username (после TrimSpace), пустой content.
// Для корня также обязателен newsID.
func TestService_CreateComment_Validation(t *testing.T) {
	s, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	// empty userID
	_, err := s.CreateComment(context.Background(), CreateCommentInput{
		NewsID: uuid.New(), UserID: uuid.Nil, Username: "x", Content: "x",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	// username -> TrimSpace -> пусто
	_, err = s.CreateComment(context.Background(), CreateCommentInput{
		NewsID: uuid.New(), UserID: uuid.New(), Username: "   ", Content: "ok",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	// content -> TrimSpace -> пусто
	_, err = s.CreateComment(context.Background(), CreateCommentInput{
		NewsID: uuid.New(), UserID: uuid.New(), Username: "u", Content: "   ",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)

	// корень: пустой newsID
	_, err = s.CreateComment(context.Background(), CreateCommentInput{
		NewsID: uuid.Nil, ParentID: "", UserID: uuid.New(), Username: "u", Content: "ok",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: ошибки уровня стораджа должны транслироваться в сервисные.
func TestService_CreateComment_StorageErrors(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	in := CreateCommentInput{
		NewsID: uuid.New(), UserID: uuid.New(), Username: "u", Content: "ok",
	}

	// ParentNotFound
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.Any()).
		Return(nil, storage.ErrParentNotFound)
	_, err := s.CreateComment(context.Background(), CreateCommentInput{
		ParentID: "507f1f77bcf86cd799439011", // reply, NewsID можно не передавать
		UserID:   in.UserID, Username: in.Username, Content: in.Content,
	})
	require.ErrorIs(t, err, ErrParentNotFound)

	// ThreadExpired
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.Any()).
		Return(nil, storage.ErrThreadExpired)
	_, err = s.CreateComment(context.Background(), CreateCommentInput{
		ParentID: "507f1f77bcf86cd799439012",
		UserID:   in.UserID, Username: in.Username, Content: in.Content,
	})
	require.ErrorIs(t, err, ErrThreadExpired)

	// MaxDepthExceeded
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.Any()).
		Return(nil, storage.ErrMaxDepthExceeded)
	_, err = s.CreateComment(context.Background(), CreateCommentInput{
		ParentID: "507f1f77bcf86cd799439013",
		UserID:   in.UserID, Username: in.Username, Content: in.Content,
	})
	require.ErrorIs(t, err, ErrMaxDepthExceeded)

	// Conflict
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.Any()).
		Return(nil, storage.ErrConflict)
	_, err = s.CreateComment(context.Background(), in)
	require.ErrorIs(t, err, ErrConflict)

	// Internal (любая иная)
	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("db down"))
	_, err = s.CreateComment(context.Background(), in)
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: успешное создание корневого комментария, проверяем TrimSpace полей и корректность передачи аргументов.
func TestService_CreateComment_OK_Root(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	newsID := uuid.New()
	uid := uuid.New()

	in := CreateCommentInput{
		NewsID:   newsID,
		UserID:   uid,
		Username: "  alice  ",
		Content:  "  hello  ",
	}

	want := mustComment(newsID, "", "alice", "hello")

	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.AssignableToTypeOf(models.Comment{})).
		DoAndReturn(func(_ context.Context, c models.Comment) (*models.Comment, error) {
			require.Equal(t, newsID, c.NewsID)
			require.Equal(t, "", c.ParentID)
			require.Equal(t, uid, c.UserID)
			require.Equal(t, "alice", c.Username)
			require.Equal(t, "hello", c.Content)
			return want, nil
		})

	got, err := s.CreateComment(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Happy-path: успешное создание ответа (ParentID задан), NewsID может быть нулевым — storage унаследует от родителя.
func TestService_CreateComment_OK_Reply(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	parentID := "507f1f77bcf86cd799439011"
	uid := uuid.New()

	in := CreateCommentInput{
		ParentID: parentID,
		UserID:   uid,
		Username: "bob",
		Content:  "reply",
	}

	want := mustComment(uuid.New(), parentID, "bob", "reply")

	ms.EXPECT().
		CreateComment(gomock.Any(), gomock.AssignableToTypeOf(models.Comment{})).
		DoAndReturn(func(_ context.Context, c models.Comment) (*models.Comment, error) {
			require.Equal(t, parentID, c.ParentID)
			require.Equal(t, uid, c.UserID)
			require.Equal(t, "bob", c.Username)
			require.Equal(t, "reply", c.Content)
			// NewsID может быть нулевым на входе для ответов — это ок.
			return want, nil
		})

	got, err := s.CreateComment(context.Background(), in)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Валидация: пустой id -> ErrInvalidArgument.
func TestService_DeleteComment_InvalidArgument(t *testing.T) {
	s, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	err := s.DeleteComment(context.Background(), "   ")
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: storage.ErrNotFound -> ErrNotFound; прочее -> ErrInternal.
func TestService_DeleteComment_Mapping(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	// NotFound
	ms.EXPECT().DeleteComment(gomock.Any(), "42").Return(storage.ErrNotFound)
	err := s.DeleteComment(context.Background(), "42")
	require.ErrorIs(t, err, ErrNotFound)

	// Internal
	ms.EXPECT().DeleteComment(gomock.Any(), "42").Return(errors.New("db down"))
	err = s.DeleteComment(context.Background(), "42")
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: успешное мягкое удаление.
func TestService_DeleteComment_OK(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	ms.EXPECT().DeleteComment(gomock.Any(), "55").Return(nil)
	require.NoError(t, s.DeleteComment(context.Background(), "55"))
}

// Валидация: пустой id -> ErrInvalidArgument.
func TestService_CommentByID_InvalidArgument(t *testing.T) {
	s, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	_, err := s.CommentByID(context.Background(), "   ")
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: storage.ErrNotFound -> ErrNotFound; прочее -> ErrInternal.
func TestService_CommentByID_Mapping(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	ms.EXPECT().CommentByID(gomock.Any(), "77").Return(nil, storage.ErrNotFound)
	_, err := s.CommentByID(context.Background(), "77")
	require.ErrorIs(t, err, ErrNotFound)

	ms.EXPECT().CommentByID(gomock.Any(), "77").Return(nil, errors.New("db down"))
	_, err = s.CommentByID(context.Background(), "77")
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: успешное чтение комментария.
func TestService_CommentByID_OK(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	want := mustComment(uuid.New(), "", "alice", "hi")
	ms.EXPECT().CommentByID(gomock.Any(), want.ID).Return(want, nil)

	got, err := s.CommentByID(context.Background(), want.ID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Валидация: пустой newsID -> ErrInvalidArgument.
func TestService_ListByNews_InvalidArgument(t *testing.T) {
	s, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	_, err := s.ListByNews(context.Background(), ListByNewsInput{
		NewsID: uuid.Nil, PageSize: 10, PageToken: "",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: storage.ErrInvalidCursor -> ErrInvalidCursor; прочее -> ErrInternal.
func TestService_ListByNews_Mapping(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	newsID := uuid.New()

	ms.EXPECT().
		ListByNews(gomock.Any(), newsID.String(), gomock.Any()).
		Return(nil, storage.ErrInvalidCursor)
	_, err := s.ListByNews(context.Background(), ListByNewsInput{
		NewsID: newsID, PageSize: 10, PageToken: "bad",
	})
	require.ErrorIs(t, err, ErrInvalidCursor)

	ms.EXPECT().
		ListByNews(gomock.Any(), newsID.String(), gomock.Any()).
		Return(nil, errors.New("db down"))
	_, err = s.ListByNews(context.Background(), ListByNewsInput{
		NewsID: newsID, PageSize: 10, PageToken: "",
	})
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: успешная выдача; проверяем, что параметры прокидываются корректно.
func TestService_ListByNews_OK(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	newsID := uuid.New()
	want := &models.Page{
		Items: []models.Comment{
			*mustComment(newsID, "", "a", "x"),
			*mustComment(newsID, "", "b", "y"),
		},
		NextPageToken: "t2",
	}

	ms.EXPECT().
		ListByNews(gomock.Any(), newsID.String(), gomock.Any()).
		DoAndReturn(func(_ context.Context, n string, p models.ListParams) (*models.Page, error) {
			require.Equal(t, newsID.String(), n)
			require.EqualValues(t, 25, p.PageSize) // из инпута ниже
			require.Equal(t, "t1", p.PageToken)    // из инпута ниже
			return want, nil
		})

	got, err := s.ListByNews(context.Background(), ListByNewsInput{
		NewsID: newsID, PageSize: 25, PageToken: "t1",
	})
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// Валидация: пустой parentID -> ErrInvalidArgument.
func TestService_ListReplies_InvalidArgument(t *testing.T) {
	s, _, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	_, err := s.ListReplies(context.Background(), ListRepliesInput{
		ParentID: "   ", PageSize: 10, PageToken: "",
	})
	require.ErrorIs(t, err, ErrInvalidArgument)
}

// Маппинг: storage.ErrInvalidCursor -> ErrInvalidCursor; прочее -> ErrInternal.
func TestService_ListReplies_Mapping(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	parentID := "507f1f77bcf86cd799439011"

	ms.EXPECT().
		ListReplies(gomock.Any(), parentID, gomock.Any()).
		Return(nil, storage.ErrInvalidCursor)
	_, err := s.ListReplies(context.Background(), ListRepliesInput{
		ParentID: parentID, PageSize: 10, PageToken: "bad",
	})
	require.ErrorIs(t, err, ErrInvalidCursor)

	ms.EXPECT().
		ListReplies(gomock.Any(), parentID, gomock.Any()).
		Return(nil, errors.New("db down"))
	_, err = s.ListReplies(context.Background(), ListRepliesInput{
		ParentID: parentID, PageSize: 10, PageToken: "",
	})
	require.ErrorIs(t, err, ErrInternal)
}

// Happy-path: успешная выдача ответов; проверяем корректную прокидку параметров.
func TestService_ListReplies_OK(t *testing.T) {
	s, ms, ctrl := newServiceWithMocks(t)
	defer ctrl.Finish()

	newsID := uuid.New()
	parentID := "507f1f77bcf86cd799439011"
	want := &models.Page{
		Items: []models.Comment{
			*mustComment(newsID, parentID, "a", "x"),
			*mustComment(newsID, parentID, "b", "y"),
		},
		NextPageToken: "t2",
	}

	ms.EXPECT().
		ListReplies(gomock.Any(), parentID, gomock.Any()).
		DoAndReturn(func(_ context.Context, pid string, p models.ListParams) (*models.Page, error) {
			require.Equal(t, parentID, pid)
			require.EqualValues(t, 50, p.PageSize)
			require.Equal(t, "t1", p.PageToken)
			return want, nil
		})

	got, err := s.ListReplies(context.Background(), ListRepliesInput{
		ParentID: parentID, PageSize: 50, PageToken: "t1",
	})
	require.NoError(t, err)
	require.Equal(t, want, got)
}
