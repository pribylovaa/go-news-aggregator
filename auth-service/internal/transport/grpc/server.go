package grpc

import (
	authv1 "auth-service/gen/go"
	"auth-service/internal/service"
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuthServer struct {
	authv1.UnimplementedAuthServiceServer
	service *service.Service
}

func NewAuthServer(service *service.Service) *AuthServer {
	return &AuthServer{service: service}
}

func (s *AuthServer) RegisterUser(ctx context.Context, req *authv1.RegisterRequest) (*authv1.AuthResponse, error) {
	const op = "transport/grpc/server/RegisterUser"

	tokenPair, uid, err := s.service.RegisterUser(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		if errors.Is(err, service.ErrEmailTaken) {
			return nil, status.Errorf(codes.AlreadyExists, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "%s: %v", op, err)
	}

	return &authv1.AuthResponse{
		UserId:       uid.String(),
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresIn:    tokenPair.ExpiresAt.Unix(),
	}, nil
}

func (s *AuthServer) LoginUser(ctx context.Context, req *authv1.LoginRequest) (*authv1.AuthResponse, error) {
	const op = "transport/grpc/server/LoginUser"

	tokenPair, uid, err := s.service.LoginUser(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			return nil, status.Errorf(codes.Unauthenticated, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "%s: %v", op, err)
	}

	return &authv1.AuthResponse{
		UserId:       uid.String(),
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresIn:    tokenPair.ExpiresAt.Unix(),
	}, nil
}

func (s *AuthServer) RefreshToken(ctx context.Context, req *authv1.RefreshTokenRequest) (*authv1.AuthResponse, error) {
	const op = "transport/grpc/server/RefreshToken"

	tokenPair, uid, err := s.service.RefreshToken(ctx, req.GetRefreshToken())
	if err != nil {
		if errors.Is(err, service.ErrInvalidToken) || errors.Is(err, service.ErrTokenExpired) {
			return nil, status.Errorf(codes.Unauthenticated, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "%s: %v", op, err)
	}

	return &authv1.AuthResponse{
		UserId:       uid.String(),
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresIn:    tokenPair.ExpiresAt.Unix(),
	}, nil
}

func (s *AuthServer) RevokeToken(ctx context.Context, req *authv1.RevokeTokenRequest) (*authv1.RevokeTokenResponse, error) {
	const op = "transport/grpc/server/RevokeToken"

	if err := s.service.RevokeToken(ctx, req.GetRefreshToken()); err != nil {
		if errors.Is(err, service.ErrInvalidToken) {
			return nil, status.Errorf(codes.NotFound, "%s: %v", op, err)
		}

		return nil, status.Errorf(codes.Internal, "%s: %v", op, err)
	}

	return &authv1.RevokeTokenResponse{Ok: true}, nil
}

func (s *AuthServer) ValidateToken(ctx context.Context, req *authv1.ValidateTokenRequest) (*authv1.ValidateTokenResponse, error) {
	const op = "transport/grpc/server/ValidateToken"

	uid, email, err := s.service.ValidateToken(ctx, req.GetAccessToken())
	if err != nil {
		if errors.Is(err, service.ErrInvalidToken) || errors.Is(err, service.ErrTokenExpired) || errors.Is(err, service.ErrTokenRevoked) {
			return &authv1.ValidateTokenResponse{Valid: false}, nil
		}

		return nil, status.Errorf(codes.Internal, "%s: %v", op, err)
	}

	return &authv1.ValidateTokenResponse{
		Valid:  true,
		UserId: uid.String(),
		Email:  email,
	}, nil
}
