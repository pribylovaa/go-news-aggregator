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
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func testCfg() config.AuthConfig {
	return config.AuthConfig{
		JWTSecret:       "unit-secret",
		AccessTokenTTL:  30 * time.Second,
		RefreshTokenTTL: 24 * time.Hour,
		Issuer:          "auth-service",
		Audience:        []string{"api-gateway"},
	}
}

func newSvc(t *testing.T) (*Service, *mocks.MockStorage, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	st := mocks.NewMockStorage(ctrl)
	svc := New(st, testCfg())
	return svc, st, ctrl
}

func mustHashPW(t *testing.T, pw string) string {
	t.Helper()
	h, err := hashPassword(pw)
	require.NoError(t, err)
	return h
}

func TestRegisterUser_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	ctx := context.Background()
	email := "User@Example.com"
	norm := "user@example.com"
	pw := "Abcdef1!"

	// Сначала UserByEmail → ErrNotFound, потом SaveUser, потом generateRefreshToken → SaveRefreshToken.
	st.EXPECT().UserByEmail(gomock.Any(), norm).Return(nil, storage.ErrNotFound)
	st.EXPECT().SaveUser(gomock.Any(), gomock.Any()).Return(nil)
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	tp, uid, err := svc.RegisterUser(ctx, email, pw)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, uid)
	require.NotEmpty(t, tp.AccessToken)
	require.NotEmpty(t, tp.RefreshToken)

	require.WithinDuration(t, time.Now().Add(svc.cfg.AccessTokenTTL), tp.AccessExpiresAt, 2*time.Second)
}

func TestRegisterUser_InvalidEmail(t *testing.T) {
	t.Parallel()

	svc, _, ctrl := newSvc(t)
	defer ctrl.Finish()

	_, _, err := svc.RegisterUser(context.Background(), "not-an-email", "Abcdef1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidEmail)
}

func TestRegisterUser_WeakOrEmptyPassword(t *testing.T) {
	t.Parallel()

	svc, _, ctrl := newSvc(t)
	defer ctrl.Finish()

	_, _, err := svc.RegisterUser(context.Background(), "u@e.com", "")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrEmptyPassword)

	_, _, err = svc.RegisterUser(context.Background(), "u@e.com", "short")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrWeakPassword)
}

func TestRegisterUser_EmailAlreadyExists_OnLookup(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	// Если UserByEmail вернул пользователя (err == nil) - считается занятым email.
	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(&models.User{ID: uuid.New(), Email: "user@example.com"}, nil)

	_, _, err := svc.RegisterUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrEmailTaken)
}

func TestRegisterUser_StorageLookupError_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(nil, errors.New("db down"))

	_, _, err := svc.RegisterUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
}

func TestRegisterUser_SaveUserAlreadyExists_MapsToEmailTaken(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(nil, storage.ErrNotFound)
	st.EXPECT().SaveUser(gomock.Any(), gomock.Any()).
		Return(storage.ErrAlreadyExists)

	_, _, err := svc.RegisterUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrEmailTaken)
}

func TestRegisterUser_SaveUserOtherError_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(nil, storage.ErrNotFound)
	st.EXPECT().SaveUser(gomock.Any(), gomock.Any()).
		Return(errors.New("insert failed"))

	_, _, err := svc.RegisterUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
}

func TestLoginUser_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	ctx := context.Background()
	email := "user@example.com"
	pw := "Abcdef1!"
	user := &models.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: mustHashPW(t, pw),
	}

	st.EXPECT().UserByEmail(gomock.Any(), email).Return(user, nil)
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	tp, uid, err := svc.LoginUser(ctx, email, pw)
	require.NoError(t, err)
	require.Equal(t, user.ID, uid)
	require.NotEmpty(t, tp.AccessToken)
	require.NotEmpty(t, tp.RefreshToken)
}

func TestLoginUser_InvalidEmail_OrEmptyPassword(t *testing.T) {
	t.Parallel()

	svc, _, ctrl := newSvc(t)
	defer ctrl.Finish()

	_, _, err := svc.LoginUser(context.Background(), "bad", "Abcdef1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	_, _, err = svc.LoginUser(context.Background(), "user@example.com", "")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLoginUser_UserNotFound_OrWrongPassword(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(nil, storage.ErrNotFound)

	_, _, err := svc.LoginUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	// wrong password
	user := &models.User{ID: uuid.New(), Email: "user@example.com", PasswordHash: mustHashPW(t, "Abcdef1!")}
	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(user, nil)

	_, _, err = svc.LoginUser(context.Background(), "user@example.com", "WRONG1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLoginUser_StorageErrorOnLookup_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(nil, errors.New("db problem"))

	_, _, err := svc.LoginUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
}

func TestRefreshToken_OK_WithRotation(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	ctx := context.Background()
	userID := uuid.New()
	user := &models.User{ID: userID, Email: "user@example.com", PasswordHash: "hash"}

	plain := "some-refresh-plain"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])

	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash,
		UserID:           userID,
		CreatedAt:        time.Now().Add(-time.Hour),
		ExpiresAt:        time.Now().Add(time.Hour),
		Revoked:          false,
	}, nil)

	st.EXPECT().UserByID(gomock.Any(), userID).Return(user, nil)

	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(true, nil)

	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	tp, uid, err := svc.RefreshToken(ctx, plain)
	require.NoError(t, err)
	require.Equal(t, userID, uid)
	require.NotEmpty(t, tp.AccessToken)
	require.NotEmpty(t, tp.RefreshToken)
}

func TestRefreshToken_NotFound_Revoked_Expired(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	plain := "r"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])

	// Not found -> ErrInvalidToken
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(nil, storage.ErrNotFound)
	_, _, err := svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)

	// Revoked
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: uuid.New(), CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: true,
	}, nil)
	_, _, err = svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenRevoked)

	// Expired
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: uuid.New(), CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Minute), Revoked: false,
	}, nil)
	_, _, err = svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenExpired)
}

func TestRefreshToken_StorageErrors_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	plain := "r"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])

	// Ошибка на чтении токена.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(nil, errors.New("db get fail"))
	_, _, err := svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)

	// Токен валиден, но UserByID падает.
	userID := uuid.New()
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID, CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: false,
	}, nil)
	st.EXPECT().UserByID(gomock.Any(), userID).Return(nil, errors.New("db user fail"))
	_, _, err = svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)

	// Ошибка при revoke старого refresh.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID, CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: false,
	}, nil)
	st.EXPECT().UserByID(gomock.Any(), userID).Return(&models.User{ID: userID, Email: "u@e.com"}, nil)
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, errors.New("db revoke fail"))
	_, _, err = svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
}

func TestRefreshToken_RotationNotFound_OrAlreadyRevoked_MapToErrors(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	ctx := context.Background()
	plain := "r"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])
	userID := uuid.New()

	// Валидация refresh ok + user ok.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID, CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: false,
	}, nil)
	st.EXPECT().UserByID(gomock.Any(), userID).Return(&models.User{ID: userID, Email: "u@e.com"}, nil)

	// При ротации старый не найден -> ErrInvalidToken.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, storage.ErrNotFound)
	_, _, err := svc.RefreshToken(ctx, plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)

	// Повтор: вернём снова валиден -> ok, но revoke = false -> ErrTokenRevoked.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID, CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: false,
	}, nil)
	st.EXPECT().UserByID(gomock.Any(), userID).Return(&models.User{ID: userID, Email: "u@e.com"}, nil)
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, nil)
	_, _, err = svc.RefreshToken(ctx, plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenRevoked)
}

func TestRevokeToken_MapErrorsAndOK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	plain := "r"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])

	// Not found -> ErrInvalidToken.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, storage.ErrNotFound)
	err := svc.RevokeToken(context.Background(), plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)

	// Already revoked (false, nil) -> ErrTokenRevoked.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, nil)
	err = svc.RevokeToken(context.Background(), plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenRevoked)

	// Другая ошибка -> пропагируется.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, errors.New("db down"))
	err = svc.RevokeToken(context.Background(), plain)
	require.Error(t, err)

	// Ok.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(true, nil)
	require.NoError(t, svc.RevokeToken(context.Background(), plain))
}

func TestValidateToken_OK(t *testing.T) {
	t.Parallel()

	svc, _, ctrl := newSvc(t)
	defer ctrl.Finish()

	ctx := context.Background()
	uid := uuid.New()
	email := "user@example.com"

	at, err := svc.generateAccessToken(ctx, uid, email, time.Now().UTC())
	require.NoError(t, err)

	gotUID, gotEmail, err := svc.ValidateToken(ctx, at)
	require.NoError(t, err)
	require.Equal(t, uid, gotUID)
	require.Equal(t, email, gotEmail)
}

func TestValidateToken_InvalidAndExpired(t *testing.T) {
	t.Parallel()

	svc, _, ctrl := newSvc(t)
	defer ctrl.Finish()

	// Неверный токен.
	_, _, err := svc.ValidateToken(context.Background(), "not-a-jwt")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)

	// Просроченный: конфиг с отрицательным TTL -> сформируем истёкший токен.
	cfg := svc.cfg
	cfg.AccessTokenTTL = -10 * time.Second
	svc.cfg = cfg

	at, err := svc.generateAccessToken(context.Background(), uuid.New(), "e@e.com", time.Now().UTC())
	require.NoError(t, err)
	_, _, err = svc.ValidateToken(context.Background(), at)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenExpired)
}
