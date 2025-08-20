package service

import (
	"auth-service/internal/config"
	"auth-service/internal/storage"
	"errors"
)

var (
	// ErrInvalidCredentials - логин/пароль неверны или пользователь не найден.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidToken - access/refresh токен не проходит формат/подпись/поиск.
	ErrInvalidToken = errors.New("invalid token")
	// ErrTokenExpired - refresh (или access при валидации) просрочен.
	ErrTokenExpired = errors.New("token expired")
	// ErrTokenRevoked - refresh отозван (logout/compromise).
	ErrTokenRevoked = errors.New("token revoked")
	// ErrEmailTaken - попытка регистрации с занятым email.
	ErrEmailTaken = errors.New("email already taken")
	// ErrRefreshTokenCollision - ошибка коллизии токена.
	ErrRefreshTokenCollision = errors.New("refresh token collision, try again")
	// ErrInvalidEmail - ошибка неверного формата почты.
	ErrInvalidEmail = errors.New("invalid email format")
	// ErrWeakPassword - ошибка слабого пароля.
	ErrWeakPassword = errors.New("password is too weak")
	// ErrEmptyPassword - ошибка пустого поля при вводе пароля.
	ErrEmptyPassword = errors.New("password is empty")
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
