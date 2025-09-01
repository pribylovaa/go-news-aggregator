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

func applyRefreshMigration(t *testing.T, st *Storage) {
	t.Helper()
	_, err := st.db.Exec(context.Background(), readMigration(t, "2_init_refresh_tokens.up.sql"))
	require.NoError(t, err, "apply 2_init_refresh_tokens.up.sql")
}

// seedUser создаёт пользователя.
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

// hashRefresh - helper для вычисления hash из plain (sha256 → base64url).
func hashRefresh(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

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

	// Повтор с тем же token_hash.
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

func TestIntegration_RefreshTokenByHash_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()
	applyRefreshMigration(t, st)

	_, err := st.RefreshTokenByHash(context.Background(), hashRefresh("missing"))
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

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

	// 1) Активный токен — должен отозваться: (true, nil).
	ok, err := st.RevokeRefreshToken(ctx, hash)
	require.NoError(t, err)
	require.True(t, ok)

	// Проверка, что в БД он теперь revoked=true.
	got, err := st.RefreshTokenByHash(ctx, hash)
	require.NoError(t, err)
	require.True(t, got.Revoked)

	// 2) Повторная попытка — уже отозван: (false, nil).
	ok, err = st.RevokeRefreshToken(ctx, hash)
	require.NoError(t, err)
	require.False(t, ok)

	// 3) Не существует — (false, ErrNotFound).
	ok, err = st.RevokeRefreshToken(ctx, hashRefresh("absent"))
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
	require.False(t, ok)
}

func TestIntegration_DeleteExpiredTokens_DeletesOnlyExpired(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()
	applyRefreshMigration(t, st)

	ctx := context.Background()
	userID := seedUser(t, st, "user@example.com")
	now := time.Now().UTC()

	// Токен A — истёк в прошлом -> должен быть удалён.
	hashA := hashRefresh("expired-past")
	require.NoError(t, st.SaveRefreshToken(ctx, &models.RefreshToken{
		RefreshTokenHash: hashA, UserID: userID,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Minute), Revoked: false,
	}))

	// Токен B — expires_at == now -> должен быть удалён.
	hashB := hashRefresh("expired-now")
	require.NoError(t, st.SaveRefreshToken(ctx, &models.RefreshToken{
		RefreshTokenHash: hashB, UserID: userID,
		CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now, Revoked: false,
	}))

	// Токен C — в будущем -> должен остаться.
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
