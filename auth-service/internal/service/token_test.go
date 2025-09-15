package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/auth-service/mocks"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Файл unit-тестов для token.go (выпуск/валидация токенов).
// Покрытие:
//   - generateAccessToken/validateAccessToken: happy-path, неправильный алгоритм, неверные issuer/audience,
//     просроченный токен, неверный формат uid в клеймах, неверный секрет подписи,
//     а также проверка leeway (exp/iat немного в прошлом — токен валиден).
//   - generateRefreshToken: сохранение хэша, соблюдение TTL, ретраи при коллизии, формат plain (base64url, без паддинга),
//     ошибки стораджа и исчерпание ретраев.
//   - validateRefreshToken: NotFound → ErrInvalidToken, Revoked -> ErrTokenRevoked,
//     Expired (в т.ч. граничный случай expires_at == now), и прокидывание ошибок стораджа.

// testAuthCfg — минимальная конфигурация для unit-тестов token.go
func testAuthCfg() config.AuthConfig {
	return config.AuthConfig{
		JWTSecret:       "unit-test-secret",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 24 * time.Hour,
		Issuer:          "auth-service",
		Audience:        []string{"api-gateway"},
	}
}

// newServiceWithMock — фабрика Service с gomock-хранилищем для тестов.
func newServiceWithMock(t *testing.T) (*Service, *mocks.MockStorage, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockSt := mocks.NewMockStorage(ctrl)
	svc := New(mockSt, testAuthCfg())
	return svc, mockSt, ctrl
}

// TestGenerateAccessToken_AndValidate_OK — happy-path: выпускаем access JWT и валидируем; сверяем uid/email.
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

// TestValidateAccessToken_WrongAlg_WrongIssuer_WrongAudience —
// некорректный алгоритм/issuer/audience приводят к ErrInvalidToken.
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

// TestValidateAccessToken_Expired — токен с отрицательным TTL (просроченный) -> ErrTokenExpired.
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

// TestValidateAccessToken_InvalidUIDClaim — некорректный uid/sub в клеймах -> ErrInvalidToken.
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

// TestGenerateRefreshToken_SavesHash_AndRespectsTTL — проверяем сохранение SHA-256 хэша и корректный TTL.
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

// TestGenerateRefreshToken_CollisionRetries_ThenSuccess — первая попытка -> ErrAlreadyExists, вторая — успешна.
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

// TestGenerateRefreshToken_CollisionExceeded_ReturnsErr — 5 подряд коллизий -> ErrRefreshTokenCollision.
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

// TestGenerateRefreshToken_StorageOtherError_IsPropagated — иные ошибки стораджа прокидываются как есть.
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

// TestValidateRefreshToken_Success — валидный plain-рефреш -> успешный lookup и корректные поля.
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

// TestValidateRefreshToken_NotFound_ReturnsInvalidToken — отсутствие записи -> ErrInvalidToken.
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

// TestValidateRefreshToken_Revoked — revoked=true -> ErrTokenRevoked.
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

// TestValidateRefreshToken_Expired — ExpiresAt в прошлом -> ErrTokenExpired.
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

// TestValidateRefreshToken_StorageError — ошибка стораджа при lookup -> возвращается вверх.
func TestValidateRefreshToken_StorageError(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	mockSt.EXPECT().
		RefreshTokenByHash(gomock.Any(), gomock.Any()).
		Return(nil, errors.New("db query timeout"))

	_, err := svc.validateRefreshToken(context.Background(), "any")
	require.Error(t, err)
}

// TestValidateAccessToken_WrongSecret_ReturnsInvalidToken — подпись сделана другим секретом -> ErrInvalidToken.
func TestValidateAccessToken_WrongSecret_ReturnsInvalidToken(t *testing.T) {
	svc, _, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	uid := uuid.New()
	now := time.Now().UTC()

	claims := jwt.MapClaims{
		"uid":   uid.String(),
		"email": "u@e.com",
		"iss":   testAuthCfg().Issuer,
		"sub":   uid.String(),
		"aud":   testAuthCfg().Audience,
		"exp":   now.Add(testAuthCfg().AccessTokenTTL).Unix(),
		"iat":   now.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte("another-secret"))
	require.NoError(t, err)

	_, _, err = svc.validateAccessToken(signed)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)
}

// TestValidateAccessToken_Leeway_AllowsSlightSkew — exp/iat в прошлом (3s) валиден из-за leeway = 5s.
func TestValidateAccessToken_Leeway_AllowsSlightSkew(t *testing.T) {
	svc, _, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	// exp/iat немного в прошлом (3s) — допустимо из-за leeway = 5s.
	now := time.Now().UTC()
	uid := uuid.New()
	claims := jwt.MapClaims{
		"uid":   uid.String(),
		"email": "u@e.com",
		"iss":   testAuthCfg().Issuer,
		"sub":   uid.String(),
		"aud":   testAuthCfg().Audience,
		"exp":   now.Add(-3 * time.Second).Unix(),
		"iat":   now.Add(-3 * time.Second).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(testAuthCfg().JWTSecret))
	require.NoError(t, err)

	gotUID, gotEmail, err := svc.validateAccessToken(signed)
	require.NoError(t, err)
	require.Equal(t, uid, gotUID)
	require.Equal(t, "u@e.com", gotEmail)
}

// TestValidateRefreshToken_ExpiresNow_TreatedAsExpired — граничный случай: expires_at == now тоже истёк.
func TestValidateRefreshToken_ExpiresNow_TreatedAsExpired(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	// ExpiresAt == now -> считается истёкшим (проверка !ExpiresAt.After(now)).
	now := time.Now().UTC()

	mockSt.EXPECT().
		RefreshTokenByHash(gomock.Any(), gomock.Any()).
		Return(&models.RefreshToken{
			RefreshTokenHash: "h",
			UserID:           uuid.New(),
			CreatedAt:        now.Add(-time.Hour),
			ExpiresAt:        now, // ровно сейчас
			Revoked:          false,
		}, nil)

	_, err := svc.validateRefreshToken(context.Background(), "any")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenExpired)
}

// TestGenerateRefreshToken_Format_Base64URL_NoPadding — plain должен быть base64url длиной 43 без паддинга.
func TestGenerateRefreshToken_Format_Base64URL_NoPadding(t *testing.T) {
	svc, mockSt, ctrl := newServiceWithMock(t)
	defer ctrl.Finish()

	mockSt.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	plain, err := svc.generateRefreshToken(context.Background(), uuid.New())
	require.NoError(t, err)

	// 32 байта -> base64url без паддинга => длина 43, алфавит: [A-Za-z0-9_-]
	require.Len(t, plain, 43)
	require.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`), plain)
}

// fmtWrap — обёртка для имитации fmt.Errorf("%w", err) над ошибками стораджа.
func fmtWrap(err error) error { return fmt.Errorf("wrapped: %w", err) }
