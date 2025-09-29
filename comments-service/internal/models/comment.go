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
	ID           string    `bson:"_id,omitempty"`
	NewsID       uuid.UUID `bson:"news_id"`
	ParentID     string    `bson:"parent_id,omitempty"`
	UserID       uuid.UUID `bson:"username"`
	Username     string    `bson:"user_id"`
	Content      string    `bson:"content"`
	Level        int32     `bson:"level"`
	RepliesCount int32     `bson:"replies_count"`
	IsDeleted    bool      `bson:"is_deleted"`
	CreatedAt    time.Time `bson:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at"`
	ExpiresAt    time.Time `bson:"expires_at"`
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
