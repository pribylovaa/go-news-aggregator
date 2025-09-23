package minio

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
	mclient "github.com/minio/minio-go/v7"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
)

// AvatarUploadURL генерирует presigned PUT URL для загрузки аватара.
// Валидирует contentType и contentLength согласно конфигу, формирует ключ вида
// "avatars/<userID>/<uuid>.<ext>", и возвращает также набор заголовков,
// которые клиент должен передать при PUT (будут проверены при подтверждении).
func (s *AvatarsStorage) AvatarUploadURL(ctx context.Context, userID uuid.UUID, contentType string, contentLength int64) (*storage.UploadInfo, error) {
	op := "storage/minio/avatars/AvatarUploadURL"

	if contentLength <= 0 || contentLength > s.cfg.Avatar.MaxSizeBytes {
		return nil, storage.ErrInvalidArgument
	}

	if !isAllowedContentType(s.cfg.Avatar.AllowedContentTypes, contentType) {
		return nil, storage.ErrInvalidArgument
	}

	var ext string
	switch contentType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	default:
		ext = ""
	}

	// Генерация ключа вида: avatars/<userID>/<uuid>.<ext>
	key := path.Join("avatars", userID.String(), uuid.NewString()+ext)

	url, err := s.client.PresignedPutObject(ctx, s.cfg.S3.Bucket, key, s.cfg.S3.PresignTTL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	info := &storage.UploadInfo{
		UploadURL: url.String(),
		AvatarKey: key,
		Expires:   s.cfg.S3.PresignTTL,
		RequiredHeader: map[string]string{
			"Content-Type":   contentType,
			"Content-Length": fmt.Sprintf("%d", contentLength),
		},
	}

	return info, nil
}

// CheckAvatarUpload подтверждает факт загрузки по key:
// проверяет, что объект существует и удовлетворяет ограничениям размера/типа.
// Возвращает публичный URL (если PublicBaseURL задан), иначе — пустую строку.
func (s *AvatarsStorage) CheckAvatarUpload(ctx context.Context, userID uuid.UUID, key string) (publicURL string, err error) {
	op := "storage/minio/avatars/CheckAvatarUpload"

	prefix := "avatars/" + userID.String() + "/"
	if !strings.HasPrefix(key, prefix) {
		return "", storage.ErrInvalidArgument
	}

	objInfo, err := s.client.StatObject(ctx, s.cfg.S3.Bucket, key, mclient.StatObjectOptions{})
	if err != nil {
		errResp := mclient.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" || errResp.StatusCode == 404 {
			return "", storage.ErrNotFoundAvatar
		}

		return "", fmt.Errorf("%s: %w", op, err)
	}

	if objInfo.Size <= 0 || objInfo.Size > s.cfg.Avatar.MaxSizeBytes {
		return "", storage.ErrInvalidArgument
	}

	if ct := objInfo.ContentType; ct != "" && !isAllowedContentType(s.cfg.Avatar.AllowedContentTypes, ct) {
		return "", storage.ErrInvalidArgument
	}

	if s.cfg.S3.PublicBaseURL == "" {
		return "", nil
	}

	base := strings.TrimRight(s.cfg.S3.PublicBaseURL, "/")

	return base + "/" + key, nil
}

// isAllowedContentType проверяет, что тип содержимого входит в allow-list.
func isAllowedContentType(allow []string, contentType string) bool {
	for _, a := range allow {
		if a == contentType {
			return true
		}
	}

	return false
}
