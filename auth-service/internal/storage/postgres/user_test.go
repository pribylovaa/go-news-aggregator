package postgres

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"auth-service/internal/models"
	"auth-service/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func repoRootFromThisFile() string {
	// internal/storage/postgres/... -> подняться на 3 уровня до корня.
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	root := repoRootFromThisFile()
	path := filepath.Join(root, "migrations", name)
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read migration %s", path)
	return string(b)
}

func startPostgres(t *testing.T) (*Storage, func()) {
	t.Helper()
	if os.Getenv("GO_TEST_INTEGRATION") == "" {
		t.Skip("integration tests are disabled (set GO_TEST_INTEGRATION=1)")
	}

	ctx := context.Background()
	req := tc.ContainerRequest{
		Image:        "postgres:16-alpine",
		Env:          map[string]string{"POSTGRES_USER": "user", "POSTGRES_PASSWORD": "pass", "POSTGRES_DB": "db"},
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	c, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)

	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5432/tcp")
	dsn := fmt.Sprintf("postgres://user:pass@%s:%s/db?sslmode=disable", host, port.Port())

	// Применяем миграции.
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, readMigration(t, "1_init_users.up.sql"))
	require.NoError(t, err)

	st, err := New(ctx, dsn)
	require.NoError(t, err)

	cleanup := func() {
		st.Close()
		_ = c.Terminate(context.Background())
	}
	return st, cleanup
}

func TestIntegration_SaveUser_And_GetByEmail_And_ByID_OK(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	ctx := context.Background()

	now := time.Now().UTC()
	u := &models.User{
		ID:           uuid.New(),
		Email:        "User@Example.com",
		PasswordHash: "hash",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	require.NoError(t, st.SaveUser(ctx, u))

	gotByEmail, err := st.UserByEmail(ctx, "user@example.com")
	require.NoError(t, err)
	require.Equal(t, u.ID, gotByEmail.ID)
	require.Equal(t, u.Email, gotByEmail.Email)
	require.True(t, strings.EqualFold("user@example.com", gotByEmail.Email))
	require.Equal(t, "hash", gotByEmail.PasswordHash)
	require.WithinDuration(t, now, gotByEmail.CreatedAt, 2*time.Second)
	require.WithinDuration(t, now, gotByEmail.UpdatedAt, 2*time.Second)

	gotUpper, err := st.UserByEmail(ctx, "USER@EXAMPLE.COM")
	require.NoError(t, err)
	require.Equal(t, u.ID, gotUpper.ID)

	gotByID, err := st.UserByID(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, u.Email, gotByID.Email)
}

func TestIntegration_SaveUser_UniqueEmail_CaseInsensitive_Violation(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	ctx := context.Background()

	// Первый пользователь.
	a := &models.User{
		ID:           uuid.New(),
		Email:        "User@Example.com",
		PasswordHash: "h1",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	require.NoError(t, st.SaveUser(ctx, a))

	// Второй — такой же e-mail, но в другом регистре -> UNIQUE violation на CITEXT.
	b := &models.User{
		ID:           uuid.New(),
		Email:        "user@example.com",
		PasswordHash: "h2",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	err := st.SaveUser(ctx, b)
	require.Error(t, err)

	require.ErrorIs(t, err, storage.ErrAlreadyExists)
}

func TestIntegration_UserByEmail_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.UserByEmail(context.Background(), "missing@example.com")
	require.Error(t, err)

	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestIntegration_UserByID_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.UserByID(context.Background(), uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}
