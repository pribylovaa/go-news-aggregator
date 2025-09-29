// Package grpc содержит реализацию gRPC-эндпоинтов CommentsService.
//
// Принципы:
//   - Контекст запроса прокидывается в сервис без потерь;
//   - Входные данные валидируются на уровне транспорта (например, UUID);
//   - Ошибки сервиса маппятся в коды gRPC:
//     ErrInvalidArgument  -> codes.InvalidArgument
//     ErrNotFound         -> codes.NotFound
//     ErrConflict         -> codes.AlreadyExists
//     ErrParentNotFound   -> codes.NotFound
//     ErrThreadExpired    -> codes.FailedPrecondition
//     ErrMaxDepthExceeded -> codes.FailedPrecondition
//     ErrInvalidCursor    -> codes.InvalidArgument
//     иные                -> codes.Internal (единое безопасное сообщение).
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
	"google.golang.org/protobuf/types/known/emptypb"
)

// CommentsServer — gRPC-сервер CommentsService.
type CommentsServer struct {
	commentsv1.UnimplementedCommentsServiceServer
	service *service.Service
}

// NewCommentsServer создаёт gRPC-сервер CommentsService.
func NewCommentsServer(svc *service.Service) *CommentsServer {
	return &CommentsServer{service: svc}
}

// CreateComment создаёт корневой комментарий или ответ.
func (s *CommentsServer) CreateComment(ctx context.Context, req *commentsv1.CreateCommentRequest) (*commentsv1.Comment, error) {
	const op = "transport/grpc/comments/CreateComment"

	// user_id обязателен и должен быть UUID.
	userID, err := uuid.Parse(strings.TrimSpace(req.GetUserId()))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s: invalid user_id: %v", op, err)
	}

	// Если parent_id пуст — это корневой комментарий, news_id обязателен (UUID).
	var newsID uuid.UUID
	parentID := strings.TrimSpace(req.GetParentId())
	newsStr := strings.TrimSpace(req.GetNewsId())
	if parentID == "" {
		newsID, err = uuid.Parse(newsStr)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "%s: invalid news_id: %v", op, err)
		}
	} else if newsStr != "" {
		// Если клиент всё-таки передал news_id для ответа — парсим и передаём (сервис унаследует от родителя).
		if parsed, perr := uuid.Parse(newsStr); perr == nil {
			newsID = parsed
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

	return toProtoComment(*res), nil
}

// DeleteComment выполняет мягкое удаление комментария по ID.
func (s *CommentsServer) DeleteComment(ctx context.Context, req *commentsv1.DeleteCommentRequest) (*emptypb.Empty, error) {
	const op = "transport/grpc/comments/DeleteComment"

	id := strings.TrimSpace(req.GetId())

	err := s.service.DeleteComment(ctx, id)
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

	return &emptypb.Empty{}, nil
}

// CommentByID возвращает комментарий по идентификатору.
func (s *CommentsServer) CommentByID(ctx context.Context, req *commentsv1.CommentByIDRequest) (*commentsv1.Comment, error) {
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

	return toProtoComment(*res), nil
}

// ListByNews возвращает страницу корневых комментариев по идентификатору новости.
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

	// Конвертация без вспомогательной функции: собираем массив вручную.
	comments := make([]*commentsv1.Comment, 0, len(page.Items))
	for i := range page.Items {
		comments = append(comments, toProtoComment(page.Items[i]))
	}

	out := &commentsv1.ListByNewsResponse{
		Comments:      comments,
		NextPageToken: page.NextPageToken,
	}
	return out, nil
}

// ListReplies возвращает страницу ответов для одного parent_id.
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

	// Конвертация без вспомогательной функции: собираем массив вручную.
	comments := make([]*commentsv1.Comment, 0, len(page.Items))
	for i := range page.Items {
		comments = append(comments, toProtoComment(page.Items[i]))
	}

	out := &commentsv1.ListRepliesResponse{
		Comments:      comments,
		NextPageToken: page.NextPageToken,
	}
	return out, nil
}

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
