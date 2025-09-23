package minio

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	mclient "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Интеграционные тесты для пакета minio:
// — поднимают реальный MinIO через testcontainers-go;
// — создают бакет для аватаров;
// — проверяют:
//    New: успешное подключение и ошибку при отсутствии бакета;
//    AvatarUploadURL: выдачу presigned PUT и валидации по типу/размеру;
//    CheckAvatarUpload: подтверждение существующего объекта, сбор публичного URL,
//    и ошибки на "чужой" ключ/несуществующий объект.
//
// Запуск:
//   GO_TEST_INTEGRATION=1 go test ./internal/storage/minio -v -race -count=1

func startMinio(t *testing.T, createBucket bool) (*AvatarsStorage, func(), string) {
	t.Helper()
	if os.Getenv("GO_TEST_INTEGRATION") == "" {
		t.Skip("integration tests are disabled (set GO_TEST_INTEGRATION=1)")
	}

	ctx := context.Background()
	const (
		image        = "docker.io/minio/minio:latest"
		rootUser     = "root"
		rootPassword = "rootpass"
		bucket       = "avatars"
	)
	req := tc.ContainerRequest{
		Image: image,
		Env: map[string]string{
			"MINIO_ROOT_USER":     rootUser,
			"MINIO_ROOT_PASSWORD": rootPassword,
		},
		Cmd:          []string{"server", "/data", "--console-address", ":9001"},
		ExposedPorts: []string{"9000/tcp", "9001/tcp"},
		WaitingFor:   wait.ForListeningPort("9000/tcp").WithStartupTimeout(60 * time.Second),
	}
	t.Logf("starting minio container with image=%q", image)
	c, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)

	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "9000/tcp")
	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())

	if createBucket {
		admin, err := mclient.New(host+":"+port.Port(), &mclient.Options{
			Creds:  credentials.NewStaticV4(rootUser, rootPassword, ""),
			Secure: false,
		})
		require.NoError(t, err)
		err = admin.MakeBucket(ctx, bucket, mclient.MakeBucketOptions{Region: "us-east-1"})
		require.NoError(t, err)
	}

	cfg := &config.Config{
		S3: config.S3Config{
			Endpoint:      endpoint,
			RootUser:      rootUser,
			RootPassword:  rootPassword,
			Bucket:        bucket,
			PresignTTL:    2 * time.Minute,
			PublicBaseURL: "http://cdn.local",
		},
		Avatar: config.AvatarConfig{
			MaxSizeBytes:        1 << 20, // 1 MiB
			AllowedContentTypes: []string{"image/png", "image/jpeg", "image/webp"},
		},
	}

	st, newErr := New(ctx, cfg)
	if !createBucket {
		require.Error(t, newErr)
		_ = c.Terminate(context.Background())
		return nil, func() {}, ""
	}
	require.NoError(t, newErr)

	cleanup := func() {
		_ = c.Terminate(context.Background())
	}
	return st, cleanup, endpoint
}

func TestIntegration_New_BucketMustExist(t *testing.T) {
	// Без предварительного создания бакета New должен вернуть ошибку.
	_, _, _ = startMinio(t, false)
}

func TestIntegration_AvatarUploadURL_And_CheckAvatarUpload_OK(t *testing.T) {
	st, cleanup, _ := startMinio(t, true)
	defer cleanup()

	uid := uuid.New()

	const bodySize = 5
	ui, err := st.AvatarUploadURL(context.Background(), uid, "image/png", bodySize)
	require.NoError(t, err)
	require.NotEmpty(t, ui.UploadURL)
	require.NotEmpty(t, ui.AvatarKey)
	require.Contains(t, ui.AvatarKey, "avatars/"+uid.String()+"/")
	require.GreaterOrEqual(t, int(ui.Expires.Seconds()), 60)
	require.Equal(t, "image/png", ui.RequiredHeader["Content-Type"])
	require.Equal(t, strconv.Itoa(bodySize), ui.RequiredHeader["Content-Length"])

	body := bytes.Repeat([]byte{0x42}, bodySize)
	req, err := http.NewRequest(http.MethodPut, ui.UploadURL, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "image/png")
	req.ContentLength = int64(bodySize)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Less(t, resp.StatusCode, 300, "PUT must succeed")

	public, err := st.CheckAvatarUpload(context.Background(), uid, ui.AvatarKey)
	require.NoError(t, err)
	require.Equal(t, "http://cdn.local/"+ui.AvatarKey, public)
}

func TestIntegration_AvatarUploadURL_InvalidArgs(t *testing.T) {
	st, cleanup, _ := startMinio(t, true)
	defer cleanup()

	uid := uuid.New()
	// Неверный тип.
	_, err := st.AvatarUploadURL(context.Background(), uid, "image/gif", 10)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
	// Неверный размер.
	_, err = st.AvatarUploadURL(context.Background(), uid, "image/png", -1)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
}

func TestIntegration_CheckAvatarUpload_Errors(t *testing.T) {
	st, cleanup, _ := startMinio(t, true)
	defer cleanup()

	uid := uuid.New()
	other := uuid.New()

	// Ключ с "чужим" префиксом.
	_, err := st.CheckAvatarUpload(context.Background(), uid, "avatars/"+other.String()+"/x.png")
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)

	// Не существует.
	_, err = st.CheckAvatarUpload(context.Background(), uid, "avatars/"+uid.String()+"/missing.png")
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrNotFoundAvatar)
}

func TestIntegration_CheckAvatarUpload_PublicBase_TrailingSlash_OK(t *testing.T) {
	st, cleanup, _ := startMinio(t, true)
	defer cleanup()

	uid := uuid.New()
	ui, err := st.AvatarUploadURL(context.Background(), uid, "image/png", 1)
	require.NoError(t, err)

	// PUT 1 байт.
	req, err := http.NewRequest(http.MethodPut, ui.UploadURL, bytes.NewReader([]byte{0x1}))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "image/png")
	req.ContentLength = 1
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Less(t, resp.StatusCode, 300)

	st.cfg.S3.PublicBaseURL = "http://cdn.local/"
	public, err := st.CheckAvatarUpload(context.Background(), uid, ui.AvatarKey)
	require.NoError(t, err)
	require.Equal(t, "http://cdn.local/"+ui.AvatarKey, public)
}

func TestIntegration_New_EndpointWithoutScheme_OK(t *testing.T) {
	st, cleanup, endpoint := startMinio(t, true)
	defer cleanup()
	_ = st

	u, err := url.Parse(endpoint)
	require.NoError(t, err)

	cfg2 := &config.Config{
		S3: config.S3Config{
			Endpoint:      u.Host,
			RootUser:      "root",
			RootPassword:  "rootpass",
			Bucket:        "avatars",
			PresignTTL:    1 * time.Minute,
			PublicBaseURL: "http://cdn.local",
		},

		Avatar: config.AvatarConfig{
			MaxSizeBytes:        1 << 20,
			AllowedContentTypes: []string{"image/png"},
		},
	}

	s2, err := New(context.Background(), cfg2)
	require.NoError(t, err)
	_ = s2
}

func TestIntegration_AvatarUploadURL_TTL_Expires(t *testing.T) {
	_, cleanup, endpoint := startMinio(t, true)
	defer cleanup()

	cfg := &config.Config{
		S3: config.S3Config{
			Endpoint:      endpoint,
			RootUser:      "root",
			RootPassword:  "rootpass",
			Bucket:        "avatars",
			PresignTTL:    1 * time.Second,
			PublicBaseURL: "",
		},
		Avatar: config.AvatarConfig{
			MaxSizeBytes:        1 << 20,
			AllowedContentTypes: []string{"image/png"},
		},
	}
	st, err := New(context.Background(), cfg)
	require.NoError(t, err)

	uid := uuid.New()
	ui, err := st.AvatarUploadURL(context.Background(), uid, "image/png", 1)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	req, err := http.NewRequest(http.MethodPut, ui.UploadURL, bytes.NewReader([]byte{0x1}))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "image/png")
	req.ContentLength = 1
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.GreaterOrEqual(t, resp.StatusCode, 400, "expired presigned URL must be rejected")
}

func TestIntegration_CheckAvatarUpload_SizeTooBig_AfterUpload(t *testing.T) {
	st, cleanup, _ := startMinio(t, true)
	defer cleanup()

	uid := uuid.New()
	const bodySize = 8
	ui, err := st.AvatarUploadURL(context.Background(), uid, "image/png", bodySize)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPut, ui.UploadURL, bytes.NewReader(bytes.Repeat([]byte{0xAB}, bodySize)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "image/png")
	req.ContentLength = bodySize
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Less(t, resp.StatusCode, 300)

	st.cfg.Avatar.MaxSizeBytes = 4

	_, err = st.CheckAvatarUpload(context.Background(), uid, ui.AvatarKey)
	require.Error(t, err)
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
}
