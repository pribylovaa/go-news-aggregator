// grpc содержит реализацию gRPC-эндпоинтов NewsService.
//
// Принципы:
//   - Контекст запроса прокидывается в сервис без потерь;
//   - Ошибки сервиса явно транслируются в коды gRPC:
//   - ErrInvalidCursor -> codes.InvalidArgument;
//   - ErrNotFound -> codes.NotFound;
//   - иные ошибки -> codes.Internal с единым безопасным сообщением.
package grpc

import (
	"context"
	"errors"

	newsv1 "github.com/pribylovaa/go-news-aggregator/news-service/gen/go/news"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type NewsServer struct {
	newsv1.UnimplementedNewsServiceServer
	service *service.Service
}

// NewNewsServer создаёт gRPC-сервер новостей.
func NewNewsServer(svc *service.Service) *NewsServer {
	return &NewsServer{service: svc}
}

// ListNews возвращает страницу новостей.
// Маппинг ошибок:
//   - ErrInvalidCursor -> InvalidArgument;
//   - прочее -> Internal (без раскрытия деталей).
func (s *NewsServer) ListNews(ctx context.Context, req *newsv1.ListNewsRequest) (*newsv1.ListNewsResponse, error) {
	const op = "transport/grpc/server/ListNews"

	page, err := s.service.ListNews(ctx, models.ListOptions{
		Limit:     req.GetLimit(),
		PageToken: req.GetPageToken(),
	})

	if err != nil {
		if errors.Is(err, service.ErrInvalidCursor) {
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	items := make([]*newsv1.News, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, toProtoNews(item))
	}

	return &newsv1.ListNewsResponse{
		Items:         items,
		NextPageToken: page.NextPageToken,
	}, nil
}

// NewsByID возвращает новость по идентификатору.
// Маппинг ошибок:
//   - ErrNotFound -> NotFound;
//   - прочее -> Internal.
func (s *NewsServer) NewsByID(ctx context.Context, req *newsv1.NewsByIDRequest) (*newsv1.NewsByIDResponse, error) {
	const op = "transport/grpc/server/NewsByID"

	item, err := s.service.NewsByID(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	return &newsv1.NewsByIDResponse{
		Item: toProtoNews(*item),
	}, nil
}

// toProtoNews конвертирует доменную модель News в protobuf-представление.
func toProtoNews(news models.News) *newsv1.News {
	return &newsv1.News{
		Id:               news.ID.String(),
		Title:            news.Title,
		Category:         news.Category,
		ShortDescription: news.ShortDescription,
		LongDescription:  news.LongDescription,
		Link:             news.Link,
		ImageUrl:         news.ImageURL,
		PublishedAt:      news.PublishedAt.Unix(),
		FetchedAt:        news.FetchedAt.Unix(),
	}
}
