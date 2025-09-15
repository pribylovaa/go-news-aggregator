// transport/grpc содержит реализацию gRPC-эндпоинтов AuthService.
// Здесь выполняется только маппинг данных и ошибок доменного слоя (service) в gRPC.
// Вся валидация и бизнес-логика находятся в пакете service.
//
// Принципы:
//   - Контекст запроса прокидывается в сервис без потерь;
//   - Ошибки сервиса явно транслируются в коды gRPC:
//   - ErrInvalidEmail/ErrWeakPassword/ErrEmptyPassword -> codes.InvalidArgument;
//   - ErrEmailTaken -> codes.AlreadyExists;
//   - ErrInvalidCredentials -> codes.Unauthenticated;
//   - ErrInvalidToken/ErrTokenExpired/ErrTokenRevoked -> codes.Unauthenticated;
//   - иные ошибки -> codes.Internal c единым безопасным сообщением;
//   - ValidateToken при невалидном/просроченном токене НЕ возвращает RPC-ошибку, а
//     отдаёт {Valid:false} (контракт эндпоинта).
//
// Безопасность:
//   - Для codes.Internal наружу не утекают детали внутренних ошибок; подробности должны попадать в логи
//     через интерсепторы на уровне сервера.
package grpc

import (
	"context"
	"errors"

	authv1 "github.com/pribylovaa/go-news-aggregator/auth-service/gen/go/auth"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuthServer struct {
	authv1.UnimplementedAuthServiceServer
	service *service.Service
}

// NewAuthServer создаёт gRPC-сервер авторизации поверх сервисного слоя.
func NewAuthServer(service *service.Service) *AuthServer {
	return &AuthServer{service: service}
}

// RegisterUser регистрирует пользователя и возвращает пару токенов.
// Маппинг ошибок:
//   - ErrInvalidEmail/ErrWeakPassword/ErrEmptyPassword -> InvalidArgument;
//   - ErrEmailTaken -> AlreadyExists;
//   - прочее -> Internal (без раскрытия деталей).
func (s *AuthServer) RegisterUser(ctx context.Context, req *authv1.RegisterRequest) (*authv1.AuthResponse, error) {
	const op = "transport/grpc/server/RegisterUser"

	tokenPair, uid, err := s.service.RegisterUser(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		if errors.Is(err, service.ErrInvalidEmail) || errors.Is(err, service.ErrWeakPassword) || errors.Is(err, service.ErrEmptyPassword) {
			return nil, status.Errorf(codes.InvalidArgument, "%s: %v", op, err)
		}

		if errors.Is(err, service.ErrEmailTaken) {
			return nil, status.Errorf(codes.AlreadyExists, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	return &authv1.AuthResponse{
		UserId:          uid.String(),
		AccessToken:     tokenPair.AccessToken,
		RefreshToken:    tokenPair.RefreshToken,
		AccessExpiresAt: tokenPair.AccessExpiresAt.Unix(),
	}, nil
}

// LoginUser аутентифицирует пользователя и возвращает новую пару токенов.
// Маппинг ошибок:
//   - ErrInvalidCredentials -> Unauthenticated;
//   - прочее -> Internal.
func (s *AuthServer) LoginUser(ctx context.Context, req *authv1.LoginRequest) (*authv1.AuthResponse, error) {
	const op = "transport/grpc/server/LoginUser"

	tokenPair, uid, err := s.service.LoginUser(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			return nil, status.Errorf(codes.Unauthenticated, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	return &authv1.AuthResponse{
		UserId:          uid.String(),
		AccessToken:     tokenPair.AccessToken,
		RefreshToken:    tokenPair.RefreshToken,
		AccessExpiresAt: tokenPair.AccessExpiresAt.Unix(),
	}, nil
}

// RefreshToken выпускает новую пару токенов по валидному refresh-токену.
// Маппинг ошибок:
//   - ErrInvalidToken/ErrTokenExpired/ErrTokenRevoked -> Unauthenticated;
//   - прочее -> Internal.
func (s *AuthServer) RefreshToken(ctx context.Context, req *authv1.RefreshTokenRequest) (*authv1.AuthResponse, error) {
	const op = "transport/grpc/server/RefreshToken"

	tokenPair, uid, err := s.service.RefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		if errors.Is(err, service.ErrInvalidToken) || errors.Is(err, service.ErrTokenExpired) || errors.Is(err, service.ErrTokenRevoked) {
			return nil, status.Errorf(codes.Unauthenticated, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	return &authv1.AuthResponse{
		UserId:          uid.String(),
		AccessToken:     tokenPair.AccessToken,
		RefreshToken:    tokenPair.RefreshToken,
		AccessExpiresAt: tokenPair.AccessExpiresAt.Unix(),
	}, nil
}

// RevokeToken отзывает refresh-токен.
// Маппинг ошибок:
//   - ErrInvalidToken/ErrTokenExpired/ErrTokenRevoked -> Unauthenticated;
//   - прочее -> Internal.
func (s *AuthServer) RevokeToken(ctx context.Context, req *authv1.RevokeTokenRequest) (*authv1.RevokeTokenResponse, error) {
	const op = "transport/grpc/server/RevokeToken"

	if err := s.service.RevokeToken(ctx, req.GetRefreshToken()); err != nil {
		if errors.Is(err, service.ErrInvalidToken) || errors.Is(err, service.ErrTokenExpired) || errors.Is(err, service.ErrTokenRevoked) {
			return nil, status.Errorf(codes.Unauthenticated, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	return &authv1.RevokeTokenResponse{Ok: true}, nil
}

// ValidateToken валидирует access-токен (JWT).
// Контракт: при невалидном/просроченном токене RPC-ошибку не возвращает —
// отдаёт {Valid:false}. При прочих ошибках — Internal.
func (s *AuthServer) ValidateToken(ctx context.Context, req *authv1.ValidateTokenRequest) (*authv1.ValidateTokenResponse, error) {
	uid, email, err := s.service.ValidateToken(ctx, req.GetAccessToken())
	if err != nil {
		if errors.Is(err, service.ErrInvalidToken) || errors.Is(err, service.ErrTokenExpired) || errors.Is(err, service.ErrTokenRevoked) {
			return &authv1.ValidateTokenResponse{Valid: false}, nil
		}

		return nil, status.Errorf(codes.Internal, "internal server error")
	}

	return &authv1.ValidateTokenResponse{
		Valid:  true,
		UserId: uid.String(),
		Email:  email,
	}, nil
}
