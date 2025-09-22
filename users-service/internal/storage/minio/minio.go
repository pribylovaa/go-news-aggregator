// minio предоставляет реализацию storage.AvatarsStorage на базе MinIO/S3.
// minio.go - конструктор клиента MinIO: нормализует endpoint,
// настраивает Secure/creds и проверяет наличие целевого бакета.
// avatars.go — реализация методов Avatars поверх клиента MinIO:
//   - генерация presigned PUT URL для загрузки аватара;
//   - подтверждение загрузки (валидация факта, размера и типа).
package minio

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	mclient "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
)

// AvatarsStorage — адаптер MinIO для операций с аватарами.
// Хранит ссылку на конфиг и minio-go клиент.
type AvatarsStorage struct {
	cfg    *config.Config
	client *mclient.Client
}

// New создает и инициализирует клиент MinIO.
// Делает endpoint-перенастройку (убирает схему), подбирает Secure по схеме
// и выполняет fail-fast-проверку доступности бакета.
func New(ctx context.Context, cfg *config.Config) (*AvatarsStorage, error) {
	const op = "storage/minio/New"

	endpoint := cfg.S3.Endpoint
	secure := strings.HasPrefix(endpoint, "https://")

	if u, err := url.Parse(endpoint); err == nil && u.Scheme != "" {
		endpoint = u.Host
		secure = u.Scheme == "https"
	}

	client, err := mclient.New(endpoint, &mclient.Options{
		Creds:  credentials.NewStaticV4(cfg.S3.RootUser, cfg.S3.RootPassword, ""),
		Secure: secure,
	})

	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	exists, err := client.BucketExists(ctx, cfg.S3.Bucket)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if !exists {
		return nil, fmt.Errorf("%s: bucket %q does not exist", op, cfg.S3.Bucket)
	}

	return &AvatarsStorage{cfg: cfg, client: client}, nil
}

// Проверка выполнения контракта верхнего уровня.
var _ storage.AvatarsStorage = (*AvatarsStorage)(nil)
