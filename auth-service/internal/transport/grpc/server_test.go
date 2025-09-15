package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"testing"
	"time"

	authv1 "github.com/pribylovaa/go-news-aggregator/auth-service/gen/go/auth"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/service"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/auth-service/mocks"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Файл unit-тестов транспортного слоя (gRPC) для AuthService.
// Все тесты изолированы: для каждого создаётся отдельный bufconn-сервер.

// testCfg — минимальная конфигурация сервиса для тестов транспорта.
func testCfg() config.AuthConfig {
	return config.AuthConfig{
		JWTSecret:       "unit-secret",
		Issuer:          "auth-service",
		Audience:        []string{"api-gateway"},
		AccessTokenTTL:  2 * time.Second,
		RefreshTokenTTL: 1 * time.Minute,
	}
}

// newSvcWithMock — фабрика сервисного слоя с gomock-хранилищем.
func newSvcWithMock(t *testing.T) (*service.Service, *mocks.MockStorage, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	st := mocks.NewMockStorage(ctrl)
	return service.New(st, testCfg()), st, ctrl
}

// startGRPC — поднимает bufconn-gRPC-сервер с переданным сервисом
// и возвращает клиент и функцию очистки.
func startGRPC(t *testing.T, svc *service.Service) (authv1.AuthServiceClient, func()) {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	authv1.RegisterAuthServiceServer(s, NewAuthServer(svc))

	go func() { _ = s.Serve(lis) }()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }

	cc, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	cleanup := func() { _ = cc.Close(); s.Stop() }
	return authv1.NewAuthServiceClient(cc), cleanup
}

// hashPW — утилита для генерации валидного bcrypt-хеша.
func hashPW(t *testing.T, p string) string {
	t.Helper()
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	require.NoError(t, err)
	return string(b)
}

// rtHash — SHA-256 -> base64.RawURLEncoding для plain-refresh токена.
func rtHash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// TestRegisterUser_OK — happy-path регистрации:
// пользователь создаётся, возвращается корректная пара токенов.
func TestRegisterUser_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	ctx := context.Background()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(nil, storage.ErrNotFound)
	st.EXPECT().SaveUser(gomock.Any(), gomock.Any()).Return(nil)
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	resp, err := client.RegisterUser(ctx, &authv1.RegisterRequest{
		Email: "user@example.com", Password: "Abcdef1!",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.UserId)
	require.NotEmpty(t, resp.AccessToken)
	require.NotEmpty(t, resp.RefreshToken)
	require.Greater(t, resp.AccessExpiresAt, time.Now().Add(1*time.Second).Unix()-1)
}

// TestRegisterUser_InvalidArgument — невалидные входные данные -> InvalidArgument.
func TestRegisterUser_InvalidArgument(t *testing.T) {
	t.Parallel()

	svc, _, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	// невалидный email.
	_, err := client.RegisterUser(context.Background(), &authv1.RegisterRequest{
		Email: "bad", Password: "Abcdef1!",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	// слабый пароль.
	_, err = client.RegisterUser(context.Background(), &authv1.RegisterRequest{
		Email: "user@example.com", Password: "short",
	})
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestRegisterUser_AlreadyExists_And_Internal — конфликт уникальности -> AlreadyExists,
// иные ошибки -> Internal.
func TestRegisterUser_AlreadyExists_And_Internal(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	// AlreadyExists через SaveUser.
	gomock.InOrder(
		st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(nil, storage.ErrNotFound),
		st.EXPECT().SaveUser(gomock.Any(), gomock.Any()).Return(storage.ErrAlreadyExists),
	)
	_, err := client.RegisterUser(context.Background(), &authv1.RegisterRequest{
		Email: "user@example.com", Password: "Abcdef1!",
	})
	require.Error(t, err)
	require.Equal(t, codes.AlreadyExists, status.Code(err))

	// Internal — любая иная ошибка.
	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(nil, errors.New("db down"))
	_, err = client.RegisterUser(context.Background(), &authv1.RegisterRequest{
		Email: "user@example.com", Password: "Abcdef1!",
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// TestLoginUser_OK — успешный логин выдаёт пару токенов.
func TestLoginUser_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	pw := "Abcdef1!"
	u := &models.User{
		ID:           uuid.New(),
		Email:        "user@example.com",
		PasswordHash: hashPW(t, pw),
	}

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(u, nil)
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	resp, err := client.LoginUser(context.Background(), &authv1.LoginRequest{
		Email: "user@example.com", Password: pw,
	})
	require.NoError(t, err)
	require.Equal(t, u.ID.String(), resp.UserId)
	require.NotEmpty(t, resp.AccessToken)
	require.NotEmpty(t, resp.RefreshToken)
}

// TestLoginUser_Unauthenticated_And_Internal — отсутствие пользователя -> Unauthenticated,
// иная ошибка -> Internal.
func TestLoginUser_Unauthenticated_And_Internal(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	// Unauthenticated: user not found.
	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(nil, storage.ErrNotFound)
	_, err := client.LoginUser(context.Background(), &authv1.LoginRequest{
		Email: "user@example.com", Password: "Abcdef1!",
	})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// Internal: storage error.
	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(nil, errors.New("db fail"))
	_, err = client.LoginUser(context.Background(), &authv1.LoginRequest{
		Email: "user@example.com", Password: "Abcdef1!",
	})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// TestRefreshToken_OK — валидный refresh и успешная ротация -> новая пара токенов.
func TestRefreshToken_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	ctx := context.Background()
	userID := uuid.New()
	hash := rtHash("plain-rt")

	// validateRefreshToken -> найден активный, не истёкший.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash,
		UserID:           userID,
		CreatedAt:        time.Now().Add(-time.Minute),
		ExpiresAt:        time.Now().Add(time.Minute),
		Revoked:          false,
	}, nil)
	// user.
	st.EXPECT().UserByID(gomock.Any(), userID).Return(&models.User{
		ID:    userID,
		Email: "user@example.com",
	}, nil)
	// rotation: revoke old -> true, save new.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(true, nil)
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	resp, err := client.RefreshToken(ctx, &authv1.RefreshTokenRequest{RefreshToken: "plain-rt"})
	require.NoError(t, err)
	require.Equal(t, userID.String(), resp.UserId)
	require.NotEmpty(t, resp.AccessToken)
	require.NotEmpty(t, resp.RefreshToken)
}

// TestRefreshToken_Unauthenticated_And_Internal — невалидный/отозванный/просроченный -> Unauthenticated,
// внутренняя ошибка на lookup user -> Internal.
func TestRefreshToken_Unauthenticated_And_Internal(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	hash := rtHash("x")

	// Not found -> Unauthenticated.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(nil, storage.ErrNotFound)
	_, err := client.RefreshToken(context.Background(), &authv1.RefreshTokenRequest{RefreshToken: "x"})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// Revoked -> Unauthenticated.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: uuid.New(),
		CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Minute),
		Revoked: true,
	}, nil)
	_, err = client.RefreshToken(context.Background(), &authv1.RefreshTokenRequest{RefreshToken: "x"})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// Expired -> Unauthenticated.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: uuid.New(),
		CreatedAt: time.Now().Add(-2 * time.Minute), ExpiresAt: time.Now().Add(-time.Second),
		Revoked: false,
	}, nil)
	_, err = client.RefreshToken(context.Background(), &authv1.RefreshTokenRequest{RefreshToken: "x"})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// Internal: user fetch fails.
	userID := uuid.New()
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID,
		CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Minute),
		Revoked: false,
	}, nil)
	st.EXPECT().UserByID(gomock.Any(), userID).Return(nil, errors.New("db user fail"))
	_, err = client.RefreshToken(context.Background(), &authv1.RefreshTokenRequest{RefreshToken: "x"})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// TestRefreshToken_RotationErrors_MapToUnauthenticated — ошибки ротации:
// старый refresh не найден или уже отозван -> Unauthenticated.
func TestRefreshToken_RotationErrors_MapToUnauthenticated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	plain := "r"
	hash := rtHash(plain)
	userID := uuid.New()

	// Валидный старый refresh + пользователь найден.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID,
		CreatedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
		Revoked: false,
	}, nil).Times(2) // два сценария ниже
	st.EXPECT().UserByID(gomock.Any(), userID).Return(&models.User{
		ID: userID, Email: "u@e.com",
	}, nil).Times(2)

	// (1) Старый refresh не найден при revoke -> ErrInvalidToken -> Unauthenticated.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, storage.ErrNotFound)
	_, err := client.RefreshToken(context.Background(), &authv1.RefreshTokenRequest{RefreshToken: plain})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// (2) Старый уже отозван -> ErrTokenRevoked -> Unauthenticated.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, nil)
	_, err = client.RefreshToken(context.Background(), &authv1.RefreshTokenRequest{RefreshToken: plain})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))
}

// TestRevokeToken_OK_And_Unauthenticated_And_Internal — маппинг ошибок revoke:
// OK, не найдено/уже отозван (Unauthenticated), прочее (Internal).
func TestRevokeToken_OK_And_Unauthenticated_And_Internal(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	hash := rtHash("r")

	// OK.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(true, nil)
	okResp, err := client.RevokeToken(context.Background(), &authv1.RevokeTokenRequest{RefreshToken: "r"})
	require.NoError(t, err)
	require.True(t, okResp.Ok)

	// Unauthenticated: ErrNotFound -> ErrInvalidToken в сервисе.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, storage.ErrNotFound)
	_, err = client.RevokeToken(context.Background(), &authv1.RevokeTokenRequest{RefreshToken: "r"})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// Unauthenticated: уже отозван (false, nil) -> ErrTokenRevoked в сервисе.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, nil)
	_, err = client.RevokeToken(context.Background(), &authv1.RevokeTokenRequest{RefreshToken: "r"})
	require.Error(t, err)
	require.Equal(t, codes.Unauthenticated, status.Code(err))

	// Internal: любая другая ошибка.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, errors.New("db revoke fail"))
	_, err = client.RevokeToken(context.Background(), &authv1.RevokeTokenRequest{RefreshToken: "r"})
	require.Error(t, err)
	require.Equal(t, codes.Internal, status.Code(err))
}

// makeExpiredAccessToken — формирует валидный по форме, но просроченный JWT.
func makeExpiredAccessToken(t *testing.T, uid uuid.UUID, email string) string {
	t.Helper()
	cfg := testCfg()
	now := time.Now().UTC()

	claims := jwt.MapClaims{
		"uid":   uid.String(),
		"email": email,
		"iss":   cfg.Issuer,
		"sub":   uid.String(),
		"aud":   cfg.Audience,
		"iat":   now.Add(-2 * time.Minute).Unix(),
		"exp":   now.Add(-1 * time.Minute).Unix(),
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(cfg.JWTSecret))
	require.NoError(t, err)
	return signed
}

// TestValidateToken_Valid_And_Invalid_And_Expired_NoRPCErr — контракт ValidateToken:
// при невалидном/просроченном токене RPC-ошибка не возвращается, только {Valid:false}.
func TestValidateToken_Valid_And_Invalid_And_Expired_NoRPCErr(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvcWithMock(t)
	defer ctrl.Finish()
	client, done := startGRPC(t, svc)
	defer done()

	ctx := context.Background()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(nil, storage.ErrNotFound)
	st.EXPECT().SaveUser(gomock.Any(), gomock.Any()).Return(nil)
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	reg, err := client.RegisterUser(ctx, &authv1.RegisterRequest{
		Email:    "user@example.com",
		Password: "Abcdef1!",
	})
	require.NoError(t, err)
	require.NotEmpty(t, reg.AccessToken)

	// 1) Валидный токен -> Valid=true, без RPC-ошибки.
	okResp, err := client.ValidateToken(ctx, &authv1.ValidateTokenRequest{
		AccessToken: reg.AccessToken,
	})
	require.NoError(t, err)
	require.True(t, okResp.Valid)
	require.Equal(t, "user@example.com", okResp.Email)
	require.NotEmpty(t, okResp.UserId)

	// 2) Невалидный мусор -> Valid=false, без RPC-ошибки.
	badResp, err := client.ValidateToken(ctx, &authv1.ValidateTokenRequest{
		AccessToken: "not-a-jwt",
	})
	require.NoError(t, err)
	require.False(t, badResp.Valid)

	// 3) Просроченный токен -> Valid=false, без RPC-ошибки.
	expired := makeExpiredAccessToken(t, uuid.New(), "user@example.com")
	expResp, err := client.ValidateToken(ctx, &authv1.ValidateTokenRequest{
		AccessToken: expired,
	})
	require.NoError(t, err)
	require.False(t, expResp.Valid)
}
