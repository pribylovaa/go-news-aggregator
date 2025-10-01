package clients

import (
	"context"
	"fmt"
	"log/slog"

	authv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/auth"
	commentsv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/comments"
	newsv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/news"
	usersv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/users"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/clients/interceptors"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/config"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Clients агрегирует все gRPC-клиенты апстрим-сервисов.
type Clients struct {
	Auth     authv1.AuthServiceClient
	News     newsv1.NewsServiceClient
	Comments commentsv1.CommentsServiceClient
	Users    usersv1.UsersServiceClient

	conns []*grpc.ClientConn
}

// New создаёт gRPC-коннекты и клиенты для всех апстримов.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*Clients, error) {
	const op = "internal/clients/New"

	// Параметры исходящих вызовов.
	timeout := cfg.Timeouts.Service
	userAgent := "api-gateway"

	// Цепочка клиентских интерсепторов: metadata -> timeout -> logging.
	chain := grpc.WithChainUnaryInterceptor(
		interceptors.ClientWithMetadata(userAgent),
		interceptors.ClientWithTimeout(timeout),
		interceptors.ClientUnaryLoggingInterceptor(log),
	)

	// Фабрика коннектов.
	dial := func(addr string) (*grpc.ClientConn, error) {
		if addr == "" {
			return nil, fmt.Errorf("%s: empty upstream addr", op)
		}

		return grpc.NewClient(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			chain,
		)
	}

	// Адреса апстримов из конфигурации.
	authAddr := cfg.GRPC.AuthAddr
	newsAddr := cfg.GRPC.NewsAddr
	commentsAddr := cfg.GRPC.CommentsAddr
	usersAddr := cfg.GRPC.UsersAddr

	authConn, err := dial(authAddr)
	if err != nil {
		return nil, fmt.Errorf("%s: auth dial: %w", op, err)
	}

	newsConn, err := dial(newsAddr)
	if err != nil {
		_ = authConn.Close()
		return nil, fmt.Errorf("%s: news dial: %w", op, err)
	}

	commentsConn, err := dial(commentsAddr)
	if err != nil {
		_ = authConn.Close()
		_ = newsConn.Close()
		return nil, fmt.Errorf("%s: comments dial: %w", op, err)
	}

	usersConn, err := dial(usersAddr)
	if err != nil {
		_ = authConn.Close()
		_ = newsConn.Close()
		_ = commentsConn.Close()
		return nil, fmt.Errorf("%s: users dial: %w", op, err)
	}

	return &Clients{
		Auth:     authv1.NewAuthServiceClient(authConn),
		News:     newsv1.NewNewsServiceClient(newsConn),
		Comments: commentsv1.NewCommentsServiceClient(commentsConn),
		Users:    usersv1.NewUsersServiceClient(usersConn),
		conns:    []*grpc.ClientConn{authConn, newsConn, commentsConn, usersConn},
	}, nil
}

// Close закрывает все открытые коннекты.
func (c *Clients) Close() error {
	var firstErr error
	for _, conn := range c.conns {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}
