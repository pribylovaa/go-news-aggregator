package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/auth-service/mocks"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Файл unit-тестов для сервисного слоя (auth.go):
// Покрытие:
//  - RegisterUser: happy-path, валидация e-mail/пароля, занятость e-mail,
//    ошибки стораджа, а также проверка нормализации e-mail и хеширования пароля,
//    ошибка записи refresh-токена.
//  - LoginUser: happy-path, невалидный ввод, пользователь отсутствует,
//    неверный пароль, ошибки стораджа, ошибка записи refresh-токена.
//  - RefreshToken: happy-path с ротацией, NotFound/Revoked/Expired,
//    ошибки стораджа (lookup/user/revoke/save new refresh).
//  - RevokeToken: маппинг ErrNotFound/уже отозван/другая ошибка/OK.
//  - ValidateToken: валидный, невалидный, просроченный — без RPC-ошибок.

// testCfg — минимальная конфигурация для unit-тестов сервисного слоя.
func testCfg() config.AuthConfig {
	return config.AuthConfig{
		JWTSecret:       "unit-secret",
		AccessTokenTTL:  30 * time.Second,
		RefreshTokenTTL: 24 * time.Hour,
		Issuer:          "auth-service",
		Audience:        []string{"api-gateway"},
	}
}

// newSvc — фабрика Service с gomock-хранилищем.
func newSvc(t *testing.T) (*Service, *mocks.MockStorage, *gomock.Controller) {
	t.Helper()
	ctrl := gomock.NewController(t)
	st := mocks.NewMockStorage(ctrl)
	svc := New(st, testCfg())
	return svc, st, ctrl
}

// mustHashPW — утилита для генерации валидного bcrypt-хеша в тестах LoginUser.
func mustHashPW(t *testing.T, pw string) string {
	t.Helper()
	h, err := hashPassword(pw)
	require.NoError(t, err)
	return h
}

// TestRegisterUser_OK — happy-path: пользователь создан, пара токенов выдана.
func TestRegisterUser_OK(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	ctx := context.Background()
	email := "User@Example.com"
	norm := "user@example.com"
	pw := "Abcdef1!"

	// lookup -> not found, save user -> ok, save refresh -> ok
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

// TestRegisterUser_InvalidEmail — невалидный e-mail -> ErrInvalidEmail.
func TestRegisterUser_InvalidEmail(t *testing.T) {
	t.Parallel()

	svc, _, ctrl := newSvc(t)
	defer ctrl.Finish()

	_, _, err := svc.RegisterUser(context.Background(), "not-an-email", "Abcdef1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidEmail)
}

// TestRegisterUser_WeakOrEmptyPassword — пустой/слабый пароль -> ErrEmptyPassword/ErrWeakPassword.
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

// TestRegisterUser_EmailAlreadyExists_OnLookup — если UserByEmail вернул запись, e-mail занят.
func TestRegisterUser_EmailAlreadyExists_OnLookup(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(&models.User{ID: uuid.New(), Email: "user@example.com"}, nil)

	_, _, err := svc.RegisterUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrEmailTaken)
}

// TestRegisterUser_StorageLookupError_Propagated — ошибка lookup прокидывается.
func TestRegisterUser_StorageLookupError_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(nil, errors.New("db down"))

	_, _, err := svc.RegisterUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
}

// TestRegisterUser_SaveUserAlreadyExists_MapsToEmailTaken — конфликт уникальности -> ErrEmailTaken.
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

// TestRegisterUser_SaveUserOtherError_Propagated — иная ошибка SaveUser прокидывается.
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

// TestRegisterUser_SavesNormalizedEmail_AndHashedPassword —
// проверяем, что Email нормализуется до нижнего регистра, а пароль сохраняется как bcrypt-хеш.
func TestRegisterUser_SavesNormalizedEmail_AndHashedPassword(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	raw := "User@Example.COM"
	norm := "user@example.com"
	pw := "Abcdef1!"

	st.EXPECT().UserByEmail(gomock.Any(), norm).Return(nil, storage.ErrNotFound)

	var saved *models.User
	st.EXPECT().SaveUser(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, u *models.User) error {
			saved = u
			require.Equal(t, norm, u.Email)
			require.NotEmpty(t, u.PasswordHash)
			require.True(t, checkPassword(u.PasswordHash, pw))
			return nil
		})

	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(nil)

	_, _, err := svc.RegisterUser(context.Background(), raw, pw)
	require.NoError(t, err)
	require.NotNil(t, saved)
}

// TestRegisterUser_SaveRefreshTokenError_Propagated — ошибка сохранения refresh-токена прокидывается.
func TestRegisterUser_SaveRefreshTokenError_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(nil, storage.ErrNotFound)
	st.EXPECT().SaveUser(gomock.Any(), gomock.Any()).Return(nil)
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(errors.New("save refresh fail"))

	_, _, err := svc.RegisterUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
}

// TestLoginUser_OK — happy-path: валидный пароль, пара токенов выдана.
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

// TestLoginUser_InvalidEmail_OrEmptyPassword — невалидный e-mail/пустой пароль -> ErrInvalidCredentials.
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

// TestLoginUser_UserNotFound_OrWrongPassword — отсутствие пользователя/неверный пароль -> ErrInvalidCredentials.
func TestLoginUser_UserNotFound_OrWrongPassword(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(nil, storage.ErrNotFound)

	_, _, err := svc.LoginUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidCredentials)

	user := &models.User{ID: uuid.New(), Email: "user@example.com", PasswordHash: mustHashPW(t, "Abcdef1!")}
	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(user, nil)

	_, _, err = svc.LoginUser(context.Background(), "user@example.com", "WRONG1!")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

// TestLoginUser_StorageErrorOnLookup_Propagated — ошибка lookup прокидывается.
func TestLoginUser_StorageErrorOnLookup_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").
		Return(nil, errors.New("db problem"))

	_, _, err := svc.LoginUser(context.Background(), "user@example.com", "Abcdef1!")
	require.Error(t, err)
}

// TestLoginUser_SaveRefreshTokenError_Propagated — ошибка сохранения refresh-токена прокидывается.
func TestLoginUser_SaveRefreshTokenError_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	pw := "Abcdef1!"
	u := &models.User{
		ID:           uuid.New(),
		Email:        "user@example.com",
		PasswordHash: mustHashPW(t, pw),
	}

	st.EXPECT().UserByEmail(gomock.Any(), "user@example.com").Return(u, nil)
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(errors.New("save refresh fail"))

	_, _, err := svc.LoginUser(context.Background(), "user@example.com", pw)
	require.Error(t, err)
}

// TestRefreshToken_OK_WithRotation — happy-path: валидный старый refresh, ротация успешно.
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

// TestRefreshToken_NotFound_Revoked_Expired — маппинг ErrNotFound/Revoked/Expired.
func TestRefreshToken_NotFound_Revoked_Expired(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	plain := "r"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])

	// Not found -> ErrInvalidToken.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(nil, storage.ErrNotFound)
	_, _, err := svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)

	// Revoked.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: uuid.New(), CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: true,
	}, nil)
	_, _, err = svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenRevoked)

	// Expired.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: uuid.New(), CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-time.Minute), Revoked: false,
	}, nil)
	_, _, err = svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenExpired)
}

// TestRefreshToken_StorageErrors_Propagated — ошибки стораджа прокидываются (lookup/user/revoke).
func TestRefreshToken_StorageErrors_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	plain := "r"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])

	// ошибка на чтении токена.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(nil, errors.New("db get fail"))
	_, _, err := svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)

	// токен валиден, но UserByID падает.
	userID := uuid.New()
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID, CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: false,
	}, nil)
	st.EXPECT().UserByID(gomock.Any(), userID).Return(nil, errors.New("db user fail"))
	_, _, err = svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)

	// ошибка при revoke старого refresh.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID, CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: false,
	}, nil)
	st.EXPECT().UserByID(gomock.Any(), userID).Return(&models.User{ID: userID, Email: "u@e.com"}, nil)
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, errors.New("db revoke fail"))
	_, _, err = svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
}

// TestRefreshToken_RotationNotFound_OrAlreadyRevoked_MapToErrors — ротация: старый не найден/уже отозван.
func TestRefreshToken_RotationNotFound_OrAlreadyRevoked_MapToErrors(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	ctx := context.Background()
	plain := "r"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])
	userID := uuid.New()

	// валидация refresh ok + user ok.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash, UserID: userID, CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(time.Hour), Revoked: false,
	}, nil)
	st.EXPECT().UserByID(gomock.Any(), userID).Return(&models.User{ID: userID, Email: "u@e.com"}, nil)

	// при ротации старый не найден -> ErrInvalidToken.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, storage.ErrNotFound)
	_, _, err := svc.RefreshToken(ctx, plain)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)

	// повтор: снова валиден -> revoke = false -> ErrTokenRevoked.
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

// TestRefreshToken_SaveNewRefreshError_Propagated — ошибка сохранения нового refresh при ротации.
func TestRefreshToken_SaveNewRefreshError_Propagated(t *testing.T) {
	t.Parallel()

	svc, st, ctrl := newSvc(t)
	defer ctrl.Finish()

	plain := "r"
	sum := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])
	userID := uuid.New()

	// валидный старый refresh.
	st.EXPECT().RefreshTokenByHash(gomock.Any(), hash).Return(&models.RefreshToken{
		RefreshTokenHash: hash,
		UserID:           userID,
		CreatedAt:        time.Now().Add(-time.Hour),
		ExpiresAt:        time.Now().Add(time.Hour),
		Revoked:          false,
	}, nil)
	// ok.
	st.EXPECT().UserByID(gomock.Any(), userID).Return(&models.User{ID: userID, Email: "u@e.com"}, nil)
	// ok.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(true, nil)
	// ошибка внутри generateRefreshToken.
	st.EXPECT().SaveRefreshToken(gomock.Any(), gomock.Any()).Return(errors.New("save refresh fail"))

	_, _, err := svc.RefreshToken(context.Background(), plain)
	require.Error(t, err)
}

// TestRevokeToken_MapErrorsAndOK — маппинг ошибок revoke и успешный сценарий.
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

	// другая ошибка -> пропагируется.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(false, errors.New("db down"))
	err = svc.RevokeToken(context.Background(), plain)
	require.Error(t, err)

	// ok.
	st.EXPECT().RevokeRefreshToken(gomock.Any(), hash).Return(true, nil)
	require.NoError(t, svc.RevokeToken(context.Background(), plain))
}

// TestValidateToken_OK — валидный access-токен -> uid/email.
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

// TestValidateToken_InvalidAndExpired — невалидный/просроченный access-токен -> ошибки сервиса.
func TestValidateToken_InvalidAndExpired(t *testing.T) {
	t.Parallel()

	svc, _, ctrl := newSvc(t)
	defer ctrl.Finish()

	// неверный токен.
	_, _, err := svc.ValidateToken(context.Background(), "not-a-jwt")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidToken)

	// просроченный: конфиг с отрицательным TTL -> сформируем истёкший токен.
	cfg := svc.cfg
	cfg.AccessTokenTTL = -10 * time.Second
	svc.cfg = cfg

	at, err := svc.generateAccessToken(context.Background(), uuid.New(), "e@e.com", time.Now().UTC())
	require.NoError(t, err)

	_, _, err = svc.ValidateToken(context.Background(), at)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrTokenExpired)
}
