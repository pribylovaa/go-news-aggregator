package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"auth-service/internal/models"
	"auth-service/internal/storage"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Файл интеграционных тестов для пакета postgres (репозиторий refresh_token.go):
// - поднимает реальный PostgreSQL через testcontainers-go (образ postgres:16-alpine);
// - применяет миграции из ./migrations (2_init_refresh_tokens.up.sql; а также 1_init_users.up.sql — уже делает startPostgres);
// - проверяет: сохранение/чтение по хэшу, нарушение уникальности, отсутствие записи, корректный флоу ревокации,
//   удаление просроченных токенов и «протекание» отменённого контекста.
//
// Запуск локально:
//   GO_TEST_INTEGRATION=1 go test ./internal/storage/postgres -v -race -count=1

// applyRefreshMigration — применяет миграцию для таблицы refresh_tokens.
func applyRefreshMigration(t *testing.T, st *Storage) {
	t.Helper()
	_, err := st.db.Exec(context.Background(), readMigration(t, "2_init_refresh_tokens.up.sql"))
	require.NoError(t, err, "apply 2_init_refresh_tokens.up.sql")
}

// seedUser — создаёт пользователя и возвращает его ID.
func seedUser(t *testing.T, st *Storage, email string) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	u := &models.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: "hash",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, st.SaveUser(context.Background(), u))
	return u.ID
}

// hashRefresh — sha256 -> base64.RawURLEncoding для plain-строки.
func hashRefresh(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// TestIntegration_SaveRefreshToken_And_GetByHash_OK — happy-path:
// сохранение валидного токена и чтение по хэшу, сверка полей и таймстемпов.
func TestIntegration_SaveRefreshToken_And_GetByHash_OK(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()
	applyRefreshMigration(t, st)

	ctx := context.Background()
	userID := seedUser(t, st, "user@example.com")

	now := time.Now().UTC()
	plain := "plain-refresh-1"
	hash := hashRefresh(plain)

	rt := &models.RefreshToken{
		RefreshTokenHash: hash,
		UserID:           userID,
		CreatedAt:        now,
		ExpiresAt:        now.Add(1 * time.Hour),
		Revoked:          false,
	}

	require.NoError(t, st.SaveRefreshToken(ctx, rt))

	got, err := st.RefreshTokenByHash(ctx, hash)
	require.NoError(t, err)
	require.Equal(t, hash, got.RefreshTokenHash)
	require.Equal(t, userID, got.UserID)
	require.False(t, got.Revoked)
	require.WithinDuration(t, now, got.CreatedAt, 2*time.Second)
	require.WithinDuration(t, now.Add(1*time.Hour), got.ExpiresAt, 2*time.Second)
}

// TestIntegration_SaveRefreshToken_UniqueViolation — нарушение уникальности по token_hash.
func TestIntegration_SaveRefreshToken_UniqueViolation(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()
	applyRefreshMigration(t, st)

	ctx := context.Background()
	userID := seedUser(t, st, "user@example.com")

	now := time.Now().UTC()
	hash := hashRefresh("dup-refresh")

	rt1 := &models.RefreshToken{
		RefreshTokenHash: hash,
		UserID:           userID,
		CreatedAt:        now,
		ExpiresAt:        now.Add(10 * time.Minute),
		Revoked:          false,
	}
	require.NoError(t, st.SaveRefreshToken(ctx, rt1))

	rt2 := &models.RefreshToken{
		RefreshTokenHash: hash,
		UserID:           userID,
		CreatedAt:        now,
		ExpiresAt:        now.Add(20 * time.Minute),
		Revoked:          false,
	}
	err := st.SaveRefreshToken(ctx, rt2)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrAlreadyExists)
}

// TestIntegration_RefreshTokenByHash_NotFound — поиск по отсутствующему хэшу -> storage.ErrNotFound.
func TestIntegration_RefreshTokenByHash_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()
	applyRefreshMigration(t, st)

	_, err := st.RefreshTokenByHash(context.Background(), hashRefresh("missing"))
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

// TestIntegration_RefreshTokenOps_ContextCanceled — отменённый контекст должен «просачиваться»
// во все операции с ошибкой context.Canceled.
func TestIntegration_RefreshTokenOps_ContextCanceled(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sha := sha256.Sum256([]byte("ctx"))
	hash := base64.StdEncoding.EncodeToString(sha[:])
	now := time.Now().UTC()

	rt := &models.RefreshToken{
		RefreshTokenHash: hash,
		UserID:           uuid.New(),
		CreatedAt:        now,
		ExpiresAt:        now.Add(10 * time.Minute),
		Revoked:          false,
	}

	// save
	err := st.SaveRefreshToken(ctx, rt)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	// get
	_, err = st.RefreshTokenByHash(ctx, hash)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	// revoke
	_, err = st.RevokeRefreshToken(ctx, hash)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	// delete expired
	err = st.DeleteExpiredTokens(ctx, now)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

// TestIntegration_RevokeRefreshToken_Flow — активный токен -> revoke = true;
// повторная ревокация -> false; для отсутствующего хэша -> storage.ErrNotFound.
func TestIntegration_RevokeRefreshToken_Flow(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()
	applyRefreshMigration(t, st)

	ctx := context.Background()
	userID := seedUser(t, st, "user@example.com")

	now := time.Now().UTC()
	hash := hashRefresh("to-revoke")

	require.NoError(t, st.SaveRefreshToken(ctx, &models.RefreshToken{
		RefreshTokenHash: hash,
		UserID:           userID,
		CreatedAt:        now,
		ExpiresAt:        now.Add(1 * time.Hour),
		Revoked:          false,
	}))

	// aктивный токен — должен отозваться: (true, nil).
	ok, err := st.RevokeRefreshToken(ctx, hash)
	require.NoError(t, err)
	require.True(t, ok)

	// проверяем, что в БД он теперь revoked = true.
	got, err := st.RefreshTokenByHash(ctx, hash)
	require.NoError(t, err)
	require.True(t, got.Revoked)

	// повторная попытка — уже отозван: (false, nil).
	ok, err = st.RevokeRefreshToken(ctx, hash)
	require.NoError(t, err)
	require.False(t, ok)

	// отсутствующий хэш — (false, ErrNotFound).
	ok, err = st.RevokeRefreshToken(ctx, hashRefresh("absent"))
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
	require.False(t, ok)
}

// TestIntegration_RevokeRefreshToken_NotFound — ревокация отсутствующего токена возвращает storage.ErrNotFound.
func TestIntegration_RevokeRefreshToken_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()
	applyRefreshMigration(t, st)

	_, err := st.RevokeRefreshToken(context.Background(), "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

// TestIntegration_DeleteExpiredTokens_DeletesOnlyExpired — удаляются только записи с expires_at <= now.
func TestIntegration_DeleteExpiredTokens_DeletesOnlyExpired(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()
	applyRefreshMigration(t, st)

	ctx := context.Background()
	userID := seedUser(t, st, "user@example.com")
	now := time.Now().UTC()

	// истёк в прошлом -> удалится.
	hashA := hashRefresh("expired-past")
	require.NoError(t, st.SaveRefreshToken(ctx, &models.RefreshToken{
		RefreshTokenHash: hashA, UserID: userID,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Minute), Revoked: false,
	}))

	// истекает ровно в now -> удалится.
	hashB := hashRefresh("expired-now")
	require.NoError(t, st.SaveRefreshToken(ctx, &models.RefreshToken{
		RefreshTokenHash: hashB, UserID: userID,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now, Revoked: false,
	}))

	// в будущем -> останется.
	hashC := hashRefresh("not-expired")
	require.NoError(t, st.SaveRefreshToken(ctx, &models.RefreshToken{
		RefreshTokenHash: hashC, UserID: userID,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(30 * time.Minute), Revoked: false,
	}))

	require.NoError(t, st.DeleteExpiredTokens(ctx, now))

	_, err := st.RefreshTokenByHash(ctx, hashA)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)

	_, err = st.RefreshTokenByHash(ctx, hashB)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)

	_, err = st.RefreshTokenByHash(ctx, hashC)
	require.NoError(t, err)
}
