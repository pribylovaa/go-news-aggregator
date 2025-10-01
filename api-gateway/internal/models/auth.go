// Входные/выходные модели под REST, зеркалят gRPC
package models

type AuthRegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type AuthRevokeRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type AuthRevokeResponse struct {
	Ok bool `json:"ok"`
}

type AuthResponse struct {
	UserID          string `json:"user_id"`
	AccessToken     string `json:"access_token"`
	RefreshToken    string `json:"refresh_token"`
	AccessExpiresAt int64  `json:"access_expires_at"` // Unix UTC
}

type AuthValidateRequest struct {
	AccessToken string `json:"access_token"`
}

type AuthValidateResponse struct {
	Valid  bool   `json:"valid"`
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}
