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

	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Файл интеграционных тестов для пакета postgres (репозиторий user.go):
// - поднимает реальный PostgreSQL через testcontainers-go (образ postgres:16-alpine);
// - применяет миграции из ./migrations (1_init_users.up.sql);
// - проверяет happy-path (создание и поиск по email/ID), уникальность (email CITEXT и первичный ключ id);
// - валидирует сценарии отсутствия записей (storage.ErrNotFound) и корректную обработку ошибок контекста (Canceled/DeadlineExceeded).
//
// Запуск локально:
//   GO_TEST_INTEGRATION=1 go test ./internal/storage/postgres -v -race -count=1

// repoRootFromThisFile — определяет корень репозитория относительно текущего файла тестов.
// Используется для поиска SQL-миграций в каталоге ./migrations независимо от текущего рабочего каталога.
func repoRootFromThisFile() string {
	// internal/storage/postgres/... -> подняться на 3 уровня до корня.
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// readMigration — читает содержимое SQL-миграции из подкаталога ./migrations.
func readMigration(t *testing.T, name string) string {
	t.Helper()
	root := repoRootFromThisFile()
	path := filepath.Join(root, "migrations", name)
	b, err := os.ReadFile(path)
	require.NoError(t, err, "read migration %s", path)
	return string(b)
}

// startPostgres — поднимает временный экземпляр PostgreSQL через testcontainers-go,
// применяет миграцию users и возвращает инициализированное хранилище и функцию очистки.
// Если переменная окружения GO_TEST_INTEGRATION не установлена — тест пропускается.
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

	// применяем миграции.
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

// TestIntegration_SaveUser_And_GetByEmail_And_ByID_OK — happy-path:
// сохранение пользователя и последующий поиск по email и ID; проверка CITEXT (регистронезависимо) и таймстемпов.
func TestIntegration_SaveUser_And_GetByEmail_And_ByID_OK(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	now := time.Now().UTC()
	u := &models.User{
		ID:           uuid.New(),
		Email:        "User@Example.Com",
		PasswordHash: "hash",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	require.NoError(t, st.SaveUser(context.Background(), u))

	gotByEmail, err := st.UserByEmail(context.Background(), strings.ToLower(u.Email))
	require.NoError(t, err)
	require.Equal(t, strings.ToLower(u.Email), strings.ToLower(gotByEmail.Email))
	require.WithinDuration(t, u.CreatedAt, gotByEmail.CreatedAt, time.Second)
	require.WithinDuration(t, u.UpdatedAt, gotByEmail.UpdatedAt, time.Second)

	gotByID, err := st.UserByID(context.Background(), u.ID)
	require.NoError(t, err)
	require.Equal(t, u.ID, gotByID.ID)
}

// TestIntegration_SaveUser_UniqueEmail_CaseInsensitive_Violation — конфликт уникальности по email
// при различии только в регистре, ожидаем storage.ErrAlreadyExists.
func TestIntegration_SaveUser_UniqueEmail_CaseInsensitive_Violation(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	now := time.Now().UTC()

	a := &models.User{
		ID:           uuid.New(),
		Email:        "user@example.com",
		PasswordHash: "h1",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, st.SaveUser(context.Background(), a))

	b := &models.User{
		ID:           uuid.New(),
		Email:        "USER@EXAMPLE.COM", // тот же email, другой регистр
		PasswordHash: "h2",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := st.SaveUser(context.Background(), b)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrAlreadyExists)
}

// TestIntegration_SaveUser_ContextDeadlineExceeded — SaveUser с мгновенным дедлайном
// должен завершиться ошибкой context.DeadlineExceeded.
func TestIntegration_SaveUser_ContextDeadlineExceeded(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	now := time.Now().UTC()
	u := &models.User{
		ID:           uuid.New(),
		Email:        "deadline@example.com",
		PasswordHash: "hash",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	err := st.SaveUser(ctx, u)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestIntegration_SaveUser_UniqueID_Violation — конфликт уникальности по первичному ключу id,
// ожидаем storage.ErrAlreadyExists.
func TestIntegration_SaveUser_UniqueID_Violation(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	now := time.Now().UTC()
	id := uuid.New()

	a := &models.User{
		ID:           id,
		Email:        "a@example.com",
		PasswordHash: "h1",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, st.SaveUser(context.Background(), a))

	b := &models.User{
		ID:           id, // тот же id
		Email:        "b@example.com",
		PasswordHash: "h2",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := st.SaveUser(context.Background(), b)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrAlreadyExists)
}

// TestIntegration_UserByEmail_NotFound — поиск по email для отсутствующей записи,
// ожидаем storage.ErrNotFound.
func TestIntegration_UserByEmail_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.UserByEmail(context.Background(), "absent@example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

// TestIntegration_UserByID_NotFound — поиск по ID для отсутствующей записи,
// ожидаем storage.ErrNotFound.
func TestIntegration_UserByID_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.UserByID(context.Background(), uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

// TestIntegration_UserQueries_ContextCanceled — отменённый контекст должен «просочиться» в ошибки
// чтения (UserByEmail, UserByID) как context.Canceled.
func TestIntegration_UserQueries_ContextCanceled(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем заранее

	_, err := st.UserByEmail(ctx, "user@example.com")
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)

	_, err = st.UserByID(ctx, uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
