package service

import (
	"auth-service/internal/config"
	"auth-service/internal/storage"
	"errors"
)

var (
	// логин/пароль неверны или пользователь не найден
	ErrInvalidCredentials = errors.New("invalid credentials")
	// access/refresh токен не проходит формат/подпись/поиск
	ErrInvalidToken = errors.New("invalid token")
	// refresh (или access при валидации) просрочен
	ErrTokenExpired = errors.New("token expired")
	// refresh отозван (logout/compromise)
	ErrTokenRevoked = errors.New("token revoked")
	// попытка регистрации с занятым email
	ErrEmailTaken = errors.New("email already taken")
)

// Service описывает бизнес-логику auth-сервиса.
type Service struct {
	storage storage.Storage
	cfg     config.AuthConfig
}

// New создает новый сервис.
func New(storage storage.Storage, cfg config.AuthConfig) *Service {
	return &Service{
		storage: storage,
		cfg:     cfg,
	}
}
