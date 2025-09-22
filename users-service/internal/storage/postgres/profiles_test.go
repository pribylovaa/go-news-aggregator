package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Интеграционные тесты для пакета postgres (реализация профилей в profiles.go):
// — поднимают реальный PostgreSQL через testcontainers-go (образ postgres:16-alpine);
// — применяют миграции из ./migrations;
// — проверяют:
//    CreateProfile: успешную вставку и ErrAlreadyExists при повторе PK;
//    ProfileByID: успешный сценарий и ErrNotFoundProfile на отсутствующую запись;
//    UpdateProfile: частичное обновление, инкремент updated_at, no-op при пустом апдейте (updated_at всё равно сдвигается);
//    ConfirmAvatarUpload: фиксацию avatar_key/url и ErrNotFoundProfile, если записи нет;
//    поведение при истёкшем контексте (context deadline exceeded).
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

// startPostgres — поднимает PostgreSQL через testcontainers-go,
// применяет миграции users и возвращает инициализированное хранилище и функцию очистки.
// Если переменная окружения GO_TEST_INTEGRATION не установлена — тест пропускается.
func startPostgres(t *testing.T) (*ProfilesStorage, func()) {
	t.Helper()
	if os.Getenv("GO_TEST_INTEGRATION") == "" {
		t.Skip("integration tests are disabled (set GO_TEST_INTEGRATION=1)")
	}

	ctx := context.Background()
	req := tc.ContainerRequest{
		Image:        "docker.io/postgres:16-alpine",
		Env:          map[string]string{"POSTGRES_USER": "user", "POSTGRES_PASSWORD": "pass", "POSTGRES_DB": "db"},
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	t.Logf("starting postgres container with image=%q", req.Image)
	c, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		ProviderType:     tc.ProviderDocker,
	})
	require.NoError(t, err)

	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "5432/tcp")
	dsn := fmt.Sprintf("postgres://user:pass@%s:%s/db?sslmode=disable", host, port.Port())

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	_, err = pool.Exec(ctx, readMigration(t, "1_init_profiles.up.sql"))
	require.NoError(t, err)

	st, err := New(ctx, dsn)
	require.NoError(t, err)

	cleanup := func() {
		st.Close()
		_ = c.Terminate(context.Background())
	}
	return st, cleanup
}

func TestIntegration_CreateProfile_And_ProfileByID_OK(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	uid := uuid.New()
	want := models.Profile{
		UserID:   uid,
		Username: "alice",
		Age:      22,
		Country:  "LV",
		Gender:   models.GenderFemale,
	}

	created, err := st.CreateProfile(context.Background(), &want)
	require.NoError(t, err)
	require.Equal(t, uid, created.UserID)
	require.Equal(t, "alice", created.Username)
	require.EqualValues(t, 22, created.Age)
	require.Equal(t, "LV", created.Country)
	require.Equal(t, models.GenderFemale, created.Gender)
	require.WithinDuration(t, time.Now().UTC(), created.CreatedAt, 5*time.Second)
	require.WithinDuration(t, time.Now().UTC(), created.UpdatedAt, 5*time.Second)

	got, err := st.ProfileByID(context.Background(), uid)
	require.NoError(t, err)
	require.Equal(t, created, got)
}

func TestIntegration_CreateProfile_AlreadyExists(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	uid := uuid.New()
	p := models.Profile{UserID: uid, Username: "dup", Age: 1}
	_, err := st.CreateProfile(context.Background(), &p)
	require.NoError(t, err)

	_, err = st.CreateProfile(context.Background(), &p)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrAlreadyExists)
}

func TestIntegration_ProfileByID_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.ProfileByID(context.Background(), uuid.New())
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFoundProfile)
}

func TestIntegration_UpdateProfile_Partial_OK(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	uid := uuid.New()
	p := models.Profile{UserID: uid, Username: "u1", Age: 10, Country: "EE", Gender: models.GenderMale}
	orig, err := st.CreateProfile(context.Background(), &p)
	require.NoError(t, err)

	time.Sleep(1100 * time.Millisecond)

	newName := "u2"
	newAge := uint32(33)
	newCountry := "LT"
	up := storage.ProfileUpdate{
		Username: &newName,
		Age:      &newAge,
		Country:  &newCountry,
	}

	got, err := st.UpdateProfile(context.Background(), uid, up)
	require.NoError(t, err)
	require.Equal(t, uid, got.UserID)
	require.Equal(t, "u2", got.Username)
	require.EqualValues(t, 33, got.Age)
	require.Equal(t, "LT", got.Country)
	require.Equal(t, models.GenderMale, got.Gender, "gender must remain unchanged")
	require.Equal(t, orig.CreatedAt, got.CreatedAt)
	require.True(t, got.UpdatedAt.After(orig.UpdatedAt), "updated_at must increase")
}

func TestIntegration_UpdateProfile_Empty_NoOp_BumpsUpdatedAt(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	uid := uuid.New()
	p := models.Profile{UserID: uid, Username: "noop", Age: 5, Country: "LV", Gender: models.GenderOther}
	orig, err := st.CreateProfile(context.Background(), &p)
	require.NoError(t, err)

	time.Sleep(1100 * time.Millisecond)

	got, err := st.UpdateProfile(context.Background(), uid, storage.ProfileUpdate{})
	require.NoError(t, err)
	require.Equal(t, orig.Username, got.Username)
	require.Equal(t, orig.Age, got.Age)
	require.Equal(t, orig.Country, got.Country)
	require.Equal(t, orig.Gender, got.Gender)
	require.True(t, got.UpdatedAt.After(orig.UpdatedAt), "updated_at must increase even on empty update")
}

func TestIntegration_UpdateProfile_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.UpdateProfile(context.Background(), uuid.New(), storage.ProfileUpdate{Username: ptr("x")})
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFoundProfile)
}

func TestIntegration_ConfirmAvatarUpload_OK(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	uid := uuid.New()
	p := models.Profile{UserID: uid, Username: "av", Age: 1}
	orig, err := st.CreateProfile(context.Background(), &p)
	require.NoError(t, err)
	require.Equal(t, "", orig.AvatarKey)
	require.Equal(t, "", orig.AvatarURL)

	time.Sleep(1100 * time.Millisecond)

	got, err := st.ConfirmAvatarUpload(context.Background(), uid, "avatars/"+uid.String()+"/a.png", "https://cdn.example/a.png")
	require.NoError(t, err)
	require.Equal(t, "avatars/"+uid.String()+"/a.png", got.AvatarKey)
	require.Equal(t, "https://cdn.example/a.png", got.AvatarURL)
	require.True(t, got.UpdatedAt.After(orig.UpdatedAt))
}

func TestIntegration_ConfirmAvatarUpload_NotFound(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	_, err := st.ConfirmAvatarUpload(context.Background(), uuid.New(), "k", "u")
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFoundProfile)
}

func TestIntegration_CreateProfile_ContextDeadlineExceeded(t *testing.T) {
	st, cleanup := startPostgres(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	p := models.Profile{UserID: uuid.New(), Username: "deadline", Age: 1}
	_, err := st.CreateProfile(ctx, &p)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), context.DeadlineExceeded.Error()))
}

func ptr[T any](v T) *T { return &v }
