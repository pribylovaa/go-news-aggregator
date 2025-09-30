package models

type Gender int32

const (
	GenderUnspecified Gender = 0
	GenderMale        Gender = 1
	GenderFemale      Gender = 2
)

// Профиль пользователя.
type User struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Age       uint32 `json:"age"`
	AvatarURL string `json:"avatar_url"`
	AvatarKey string `json:"avatar_key"`
	CreatedAt int64  `json:"created_at"` // Unix UTC
	UpdatedAt int64  `json:"updated_at"` // Unix UTC
	Country   string `json:"country"`
	Gender    Gender `json:"gender"`
}

// Запрос на изменение профиля; поля опциональные, маска управляется на b/e.
// На REST принимаем ровно те же поля; update_mask передаём в gRPC внутри handler.
type UpdateUserRequest struct {
	UserID   string `json:"user_id"`
	Username string `json:"username,omitempty"`
	Age      uint32 `json:"age,omitempty"`
	Country  string `json:"country,omitempty"`
	Gender   Gender `json:"gender,omitempty"`
}

// Пресайн на загрузку аватара.
type AvatarPresignRequest struct {
	UserID        string `json:"user_id"`
	ContentType   string `json:"content_type"`
	ContentLength uint64 `json:"content_length"`
}

type AvatarPresignResponse struct {
	UploadURL      string            `json:"upload_url"`
	AvatarKey      string            `json:"avatar_key"`
	ExpiresSeconds uint32            `json:"expires_seconds"`
	RequiredHeader map[string]string `json:"required_headers"`
}

type AvatarConfirmRequest struct {
	UserID    string `json:"user_id"`
	AvatarKey string `json:"avatar_key"`
}
