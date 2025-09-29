// Реализация gRPC-эндпоинтов CommentsService по контракту comments.v1.
//
// Маппинг ошибок сервиса в коды gRPC:
//
//	ErrInvalidArgument        -> codes.InvalidArgument
//	ErrNotFound               -> codes.NotFound
//	ErrConflict               -> codes.AlreadyExists
//	ErrParentNotFound         -> codes.NotFound
//	ErrThreadExpired          -> codes.FailedPrecondition
//	ErrMaxDepthExceeded       -> codes.FailedPrecondition
//	ErrInvalidCursor          -> codes.InvalidArgument
//	прочее                    -> codes.Internal
package grpc

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	commentsv1 "github.com/pribylovaa/go-news-aggregator/comments-service/gen/go/comments"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/comments-service/internal/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CommentsServer — gRPC-сервер CommentsService.
type CommentsServer struct {
	commentsv1.UnimplementedCommentsServiceServer
	service *service.Service
}

func NewCommentsServer(svc *service.Service) *CommentsServer {
	return &CommentsServer{service: svc}
}

// CreateComment — создание корня или ответа.
// Возвращает CreateCommentResponse с вложенным Comment.
func (s *CommentsServer) CreateComment(ctx context.Context, req *commentsv1.CreateCommentRequest) (*commentsv1.CreateCommentResponse, error) {
	const op = "transport/grpc/comments/CreateComment"

	// user_id обязателен и должен быть UUID.
	userID, err := uuid.Parse(strings.TrimSpace(req.GetUserId()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s: invalid user_id: %v", op, err)
	}

	// Если parent_id пуст — это корневой комментарий: news_id обязателен (UUID).
	// Если parent_id не пуст — игнорируем news_id (его унаследует сторедж от родителя);
	// даже если клиент прислал news_id, на этой ветке не валидируем его, чтобы не ломать UX.
	var newsID uuid.UUID
	parentID := strings.TrimSpace(req.GetParentId())
	newsStr := strings.TrimSpace(req.GetNewsId())
	if parentID == "" {
		newsID, err = uuid.Parse(newsStr)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%s: invalid news_id: %v", op, err)
		}
	}

	res, err := s.service.CreateComment(ctx, service.CreateCommentInput{
		NewsID:   newsID,
		ParentID: parentID,
		UserID:   userID,
		Username: req.GetUsername(),
		Content:  req.GetContent(),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		case errors.Is(err, service.ErrParentNotFound), errors.Is(err, service.ErrNotFound):
			return nil, status.Errorf(codes.NotFound, "%s: %v", op, err)
		case errors.Is(err, service.ErrThreadExpired), errors.Is(err, service.ErrMaxDepthExceeded):
			return nil, status.Errorf(codes.FailedPrecondition, "%s: %v", op, err)
		case errors.Is(err, service.ErrConflict):
			return nil, status.Errorf(codes.AlreadyExists, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	return &commentsv1.CreateCommentResponse{Comment: toProtoComment(*res)}, nil
}

// DeleteComment — мягкое удаление. Возвращает пустую DeleteCommentResponse.
func (s *CommentsServer) DeleteComment(ctx context.Context, req *commentsv1.DeleteCommentRequest) (*commentsv1.DeleteCommentResponse, error) {
	const op = "transport/grpc/comments/DeleteComment"

	id := strings.TrimSpace(req.GetId())
	if err := s.service.DeleteComment(ctx, id); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		case errors.Is(err, service.ErrNotFound):
			return nil, status.Errorf(codes.NotFound, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	return &commentsv1.DeleteCommentResponse{}, nil
}

// CommentByID — вернуть комментарий по id (в обёртке CommentByIDResponse).
func (s *CommentsServer) CommentByID(ctx context.Context, req *commentsv1.CommentByIDRequest) (*commentsv1.CommentByIDResponse, error) {
	const op = "transport/grpc/comments/CommentByID"

	id := strings.TrimSpace(req.GetId())
	if id == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s: empty id", op)
	}

	res, err := s.service.CommentByID(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		case errors.Is(err, service.ErrNotFound):
			return nil, status.Errorf(codes.NotFound, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	return &commentsv1.CommentByIDResponse{Comment: toProtoComment(*res)}, nil
}

// ListByNews — страница корневых комментариев по news_id.
func (s *CommentsServer) ListByNews(ctx context.Context, req *commentsv1.ListByNewsRequest) (*commentsv1.ListByNewsResponse, error) {
	const op = "transport/grpc/comments/ListByNews"

	newsID, err := uuid.Parse(strings.TrimSpace(req.GetNewsId()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s: invalid news_id: %v", op, err)
	}

	page, err := s.service.ListByNews(ctx, service.ListByNewsInput{
		NewsID:    newsID,
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument), errors.Is(err, service.ErrInvalidCursor):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	// Собираем repeated Comment вручную.
	items := make([]*commentsv1.Comment, 0, len(page.Items))
	for i := range page.Items {
		items = append(items, toProtoComment(page.Items[i]))
	}

	return &commentsv1.ListByNewsResponse{
		Comments:      items,
		NextPageToken: page.NextPageToken,
	}, nil
}

// ListReplies — страница ответов в пределах ветки по parent_id.
func (s *CommentsServer) ListReplies(ctx context.Context, req *commentsv1.ListRepliesRequest) (*commentsv1.ListRepliesResponse, error) {
	const op = "transport/grpc/comments/ListReplies"

	parentID := strings.TrimSpace(req.GetParentId())
	if parentID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "%s: empty parent_id", op)
	}

	page, err := s.service.ListReplies(ctx, service.ListRepliesInput{
		ParentID:  parentID,
		PageSize:  req.GetPageSize(),
		PageToken: req.GetPageToken(),
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidArgument), errors.Is(err, service.ErrInvalidCursor):
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		default:
			return nil, status.Errorf(codes.Internal, "internal server error")
		}
	}

	items := make([]*commentsv1.Comment, 0, len(page.Items))
	for i := range page.Items {
		items = append(items, toProtoComment(page.Items[i]))
	}

	return &commentsv1.ListRepliesResponse{
		Comments:      items,
		NextPageToken: page.NextPageToken,
	}, nil
}

// toProtoComment — конвертация доменной модели в protobuf.
func toProtoComment(c models.Comment) *commentsv1.Comment {
	return &commentsv1.Comment{
		Id:           c.ID,
		NewsId:       c.NewsID.String(),
		ParentId:     c.ParentID,
		UserId:       c.UserID.String(),
		Username:     c.Username,
		Content:      c.Content,
		Level:        c.Level,
		RepliesCount: c.RepliesCount,
		IsDeleted:    c.IsDeleted,
		CreatedAt:    c.CreatedAt.UTC().Unix(),
		UpdatedAt:    c.UpdatedAt.UTC().Unix(),
		ExpiresAt:    c.ExpiresAt.UTC().Unix(),
	}
}
