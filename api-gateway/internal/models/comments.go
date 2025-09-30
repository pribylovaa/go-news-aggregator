package models

type Comment struct {
	ID           string `json:"id"` // Mongo ObjectID
	NewsID       string `json:"news_id"`
	ParentID     string `json:"parent_id"` // "" — корень
	UserID       string `json:"user_id"`
	Username     string `json:"username"`
	Content      string `json:"content"`
	Level        int32  `json:"level"`         // 0 — корень
	RepliesCount int32  `json:"replies_count"` // прямые дети
	IsDeleted    bool   `json:"is_deleted"`
	CreatedAt    int64  `json:"created_at"` // Unix UTC
	UpdatedAt    int64  `json:"updated_at"` // Unix UTC
	ExpiresAt    int64  `json:"expires_at"` // Unix UTC
}

// Создание (корневой или ответ).
type CreateCommentRequest struct {
	NewsID   string `json:"news_id"`
	ParentID string `json:"parent_id,omitempty"` // если задан — reply
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Content  string `json:"content"`
}

type CreateCommentResponse struct {
	Comment *Comment `json:"comment"`
}

type GetCommentRequest struct {
	ID string `json:"id"`
}
type GetCommentResponse struct {
	Comment *Comment `json:"comment"`
}

// Список корневых комментариев новости.
type ListRootCommentsRequest struct {
	NewsID    string `json:"news_id"`
	PageSize  int32  `json:"page_size"`
	PageToken string `json:"page_token"`
}
type ListRootCommentsResponse struct {
	Comments      []Comment `json:"comments"`
	NextPageToken string    `json:"next_page_token"`
}

// Список ответов на конкретный комментарий.
type ListRepliesRequest struct {
	ParentID  string `json:"parent_id"`
	PageSize  int32  `json:"page_size"`
	PageToken string `json:"page_token"`
}
type ListRepliesResponse struct {
	Comments      []Comment `json:"comments"`
	NextPageToken string    `json:"next_page_token"`
}
