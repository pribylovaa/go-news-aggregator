// Package models содержит доменные сущности comments-сервиса.
package models

import (
	"time"

	"github.com/google/uuid"
)

// Comment — внутренняя доменная модель комментария (MongoDB).
// Важно:
//   - ID — ObjectID MongoDB. Наружу/вовнутрь конвертируется в string.
//   - NewsID/UserID/Username — UUID из смежных сервисов (news-service/users-service).
//   - ParentID — ObjectID родителя.
//   - Level — глубина ветки (корень = 0). Проверяется на запись по cfg.Limits.MaxDepth.
//   - RepliesCount — количество прямых детей (для UI, может обновляться асинхронно).
//   - IsDeleted — мягкое удаление; при отдаче наружу content может маскироваться.
//   - ExpiresAt — единая «дата смерти» ветки; у ответов совпадает с корнем (TTL-индекс).
//   - CreatedAt/UpdatedAt — наружу/внутрь gRPC конвертируем в int64.
type Comment struct {
	ID           string
	NewsID       uuid.UUID
	ParentID     string
	UserID       uuid.UUID
	Username     string
	Content      string
	Level        int32
	RepliesCount int32
	IsDeleted    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ExpiresAt    time.Time
}

// ListParams — базовые параметры постраничной выдачи.
type ListParams struct {
	PageSize  int32
	PageToken string
}

// Page — результат постраничной выдачи.
type Page struct {
	Items         []Comment
	NextPageToken string
}
