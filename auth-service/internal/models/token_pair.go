package models

import "time"

// TokenPair - пара токенов для аутентификации.
type TokenPair struct {
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt time.Time
}
