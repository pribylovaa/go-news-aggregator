package service

import (
	"auth-service/internal/config"
	"auth-service/internal/models"
	"auth-service/internal/storage"
	"auth-service/mocks"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func testAuthCfg() config.AuthConfig {
	return config.AuthConfig{
		JWTSecret:       "unit-test-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 24 * time.Hour,
		Issuer:          "auth-service",
		Audience:        []string{"api-gateway"},
	}
}

func newServiceWithMock(t *testing.T) (*Service, *mocks.MockStorage, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockSt := mocks.NewMockStorage(ctrl)
	svc := New(mockSt, testAuthCfg())
	return svc, mockSt, ctrl
}

func TestGenerateAccessToken_AndValidate_OK(t *testing.T) {
	svc, _, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	ctx := context.Background()
	uid := uuid.New()
	email := "user@example.com"
	now := time.Now().UTC()

	at, err := svc.generateAccessToken(ctx, uid, email, now)
	require.NoError(t, err)

	vUID, vEmail, err := svc.validateAccessToken(at)
	require.NoError(t, err)
	require.Equal(t, uid, vUID)
	require.Equal(t, email, vEmail)
}

func TestValidateAccessToken_WrongAlg_WrongIssuer_WrongAudience(t *testing.T) {
	svc, _, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	secret := []byte(testAuthCfg().JWTSecret)
	uid := uuid.New()
	now := time.Now().UTC()

	t.Run("wrong alg", func(t *testing.T) {
		claims := jwt.MapClaims{
			"uid":   uid.String(),
			"email": "a@b.c",
			"iss":   testAuthCfg().Issuer,
			"sub":   uid.String(),
			"aud":   testAuthCfg().Audience,
			"exp":   now.Add(testAuthCfg().AccessTokenTTL).Unix(),
			"iat":   now.Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
		signed, err := token.SignedString(secret)
		require.NoError(t, err)

		_, _, err = svc.validateAccessToken(signed)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("wrong issuer", func(t *testing.T) {
		claims := jwt.MapClaims{
			"uid":   uid.String(),
			"email": "a@b.c",
			"iss":   "another-issuer",
			"sub":   uid.String(),
			"aud":   testAuthCfg().Audience,
			"exp":   now.Add(testAuthCfg().AccessTokenTTL).Unix(),
			"iat":   now.Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString(secret)
		require.NoError(t, err)

		_, _, err = svc.validateAccessToken(signed)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("wrong audience", func(t *testing.T) {
		claims := jwt.MapClaims{
			"uid":   uid.String(),
			"email": "a@b.c",
			"iss":   testAuthCfg().Issuer,
			"sub":   uid.String(),
			"aud":   []string{"unexpected-aud"},
			"exp":   now.Add(testAuthCfg().AccessTokenTTL).Unix(),
			"iat":   now.Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := token.SignedString(secret)
		require.NoError(t, err)

		_, _, err = svc.validateAccessToken(signed)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrInvalidToken)
	})
}

func TestValidateAccessToken_Expired(t *testing.T) {
	svc, _, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	cfg := testAuthCfg()
	cfg.AccessTokenTTL = -10 * time.Second
	svc.cfg = cfg

	uid := uuid.New()
	email := "user@example.com"
	now := time.Now().UTC()

	at, err := svc.generateAccessToken(context.Background(), uid, email, now)
	require.NoError(t, err)

	_, _, err = svc.validateAccessToken(at)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenExpired)
}

func TestValidateAccessToken_InvalidUIDClaim(t *testing.T) {
	svc, _, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	secret := []byte(testAuthCfg().JWTSecret)
	now := time.Now().UTC()

	claims := jwt.MapClaims{
		"uid":   "not-a-uuid",
		"email": "a@b.c",
		"iss":   testAuthCfg().Issuer,
		"sub":   "not-a-uuid",
		"aud":   testAuthCfg().Audience,
		"exp":   now.Add(testAuthCfg().AccessTokenTTL).Unix(),
		"iat":   now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	require.NoError(t, err)

	_, _, err = svc.validateAccessToken(signed)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestGenerateRefreshToken_SavesHash_AndRespectsTTL(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	ctx := context.Background()
	uid := uuid.New()

	var saved *models.RefreshToken
	mockSt.
		EXPECT().
		SaveRefreshToken(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, rt *models.RefreshToken) error {
			saved = rt
			return nil
		})

	plain, err := svc.generateRefreshToken(ctx, uid)
	require.NoError(t, err)

	sum := sha256.Sum256([]byte(plain))
	expectedHash := base64.RawURLEncoding.EncodeToString(sum[:])
	require.Equal(t, expectedHash, saved.RefreshTokenHash)

	require.WithinDuration(t, saved.CreatedAt.Add(svc.cfg.RefreshTokenTTL), saved.ExpiresAt, time.Second)

	require.Equal(t, uid, saved.UserID)
	require.False(t, saved.Revoked)
}

func TestGenerateRefreshToken_CollisionRetries_ThenSuccess(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	gomock.InOrder(
		mockSt.EXPECT().
			SaveRefreshToken(gomock.Any(), gomock.Any()).
			Return(fmtWrap(storage.ErrAlreadyExists)),
		mockSt.EXPECT().
			SaveRefreshToken(gomock.Any(), gomock.Any()).
			Return(nil),
	)

	plain, err := svc.generateRefreshToken(context.Background(), uuid.New())
	require.NoError(t, err)
	require.NotEmpty(t, plain)
}

func TestGenerateRefreshToken_CollisionExceeded_ReturnsErr(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	for i := 0; i < 5; i++ {
		mockSt.EXPECT().
			SaveRefreshToken(gomock.Any(), gomock.Any()).
			Return(fmtWrap(storage.ErrAlreadyExists))
	}

	_, err := svc.generateRefreshToken(context.Background(), uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRefreshTokenCollision)
}

func TestGenerateRefreshToken_StorageOtherError_IsPropagated(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	mockSt.EXPECT().
		SaveRefreshToken(gomock.Any(), gomock.Any()).
		Return(errors.New("db down"))

	_, err := svc.generateRefreshToken(context.Background(), uuid.New())
	require.Error(t, err)

	require.NotErrorIs(t, err, ErrRefreshTokenCollision)
}

func TestValidateRefreshToken_Success(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	uid := uuid.New()
	plain := "refresh-plain-example"
	sum := sha256.Sum256([]byte(plain))
	expectedHash := base64.RawURLEncoding.EncodeToString(sum[:])

	var lookupHash string
	mockSt.
		EXPECT().
		RefreshTokenByHash(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, h string) (*models.RefreshToken, error) {
			lookupHash = h
			return &models.RefreshToken{
				RefreshTokenHash: expectedHash,
				UserID:           uid,
				CreatedAt:        time.Now().UTC().Add(-time.Hour),
				ExpiresAt:        time.Now().UTC().Add(time.Hour),
				Revoked:          false,
			}, nil
		})

	token, err := svc.validateRefreshToken(context.Background(), plain)
	require.NoError(t, err)
	require.Equal(t, expectedHash, lookupHash)
	require.Equal(t, uid, token.UserID)
}

func TestValidateRefreshToken_NotFound_ReturnsInvalidToken(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	mockSt.EXPECT().
		RefreshTokenByHash(gomock.Any(), gomock.Any()).
		Return(nil, fmtWrap(storage.ErrNotFound))

	_, err := svc.validateRefreshToken(context.Background(), "any")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestValidateRefreshToken_Revoked(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	mockSt.EXPECT().
		RefreshTokenByHash(gomock.Any(), gomock.Any()).
		Return(&models.RefreshToken{
			RefreshTokenHash: "h",
			UserID:           uuid.New(),
			CreatedAt:        time.Now().UTC().Add(-time.Hour),
			ExpiresAt:        time.Now().UTC().Add(time.Hour),
			Revoked:          true,
		}, nil)

	_, err := svc.validateRefreshToken(context.Background(), "any")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenRevoked)
}

func TestValidateRefreshToken_Expired(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	mockSt.EXPECT().
		RefreshTokenByHash(gomock.Any(), gomock.Any()).
		Return(&models.RefreshToken{
			RefreshTokenHash: "h",
			UserID:           uuid.New(),
			CreatedAt:        time.Now().UTC().Add(-2 * time.Hour),
			ExpiresAt:        time.Now().UTC().Add(-time.Minute),
			Revoked:          false,
		}, nil)

	_, err := svc.validateRefreshToken(context.Background(), "any")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenExpired)
}

func TestValidateRefreshToken_StorageError(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	mockSt.EXPECT().
		RefreshTokenByHash(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("db query timeout"))

	_, err := svc.validateRefreshToken(context.Background(), "any")
	require.Error(t, err)
}

// fmtWrap - оборачивает ошибку из storage, имитируя fmt.Errorf("%w").
func fmtWrap(err error) error { return fmt.Errorf("wrapped: %w", err) }
