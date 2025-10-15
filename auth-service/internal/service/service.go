// service содержит бизнес-логику auth-сервиса:
// регистрацию/аутентификацию пользователей, выпуск/проверку токенов
// и работу с хранилищем через интерфейсы из пакета storage.
//
// Основные аспекты:
//   - Пакет не хранит состояние запроса внутри Service; экземпляр Service
//     безопасен для конкурентного использования из разных горутин при условии,
//     что переданное хранилище (storage.Storage) потокобезопасно.
//   - Ошибки возвращаются и далее маппятся
//     транспортом на gRPC-коды (см. комментарии к переменным ошибок ниже).
package service

import (
	"errors"

	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/cache"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/config"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/storage"
)

var (
	// ErrInvalidCredentials — пара логин/пароль неверна или пользователь не найден.
	// На уровне транспорта обычно маппится в codes.Unauthenticated (HTTP 401).
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrInvalidToken — токен (access/refresh) некорректен по формату/подписи
	// или отсутствует в хранилище. Транспорт: codes.Unauthenticated (HTTP 401).
	ErrInvalidToken = errors.New("invalid token")

	// ErrTokenExpired — срок действия токена истёк.
	// Транспорт: codes.Unauthenticated (HTTP 401).
	ErrTokenExpired = errors.New("token expired")

	// ErrTokenRevoked — токен отозван (logout/rotation/compromise) и недействителен
	// независимо от срока. Транспорт: codes.Unauthenticated (HTTP 401).
	ErrTokenRevoked = errors.New("token revoked")

	// ErrEmailTaken — e-mail уже занят другим пользователем.
	// Транспорт: codes.AlreadyExists (HTTP 409).
	ErrEmailTaken = errors.New("email already taken")

	// ErrRefreshTokenCollision — исчерпаны попытки сгенерировать уникальный refresh-токен
	// (редкий случай коллизий при сохранении хэша в БД после нескольких ретраев).
	// Транспорт: codes.Internal (HTTP 500).
	ErrRefreshTokenCollision = errors.New("refresh token collision")

	// ErrInvalidEmail — e-mail имеет некорректный формат или не проходит политику валидации.
	// Транспорт: codes.InvalidArgument (HTTP 400).
	ErrInvalidEmail = errors.New("invalid email format")

	// ErrWeakPassword — пароль не удовлетворяет политикам сложности.
	// Транспорт: codes.InvalidArgument (HTTP 400).
	ErrWeakPassword = errors.New("password is too weak")

	// ErrEmptyPassword — пароль пустой.
	// Транспорт: codes.InvalidArgument (HTTP 400).
	ErrEmptyPassword = errors.New("password is empty")
)

// Service описывает бизнес-логику auth-сервиса.
type Service struct {
	storage storage.Storage
	cfg     config.AuthConfig
	rcache  cache.RefreshCache // может быть nil, если кэш не сконфигурирован
}

// New создаёт новый экземпляр Service.
func New(storage storage.Storage, cfg config.AuthConfig) *Service {
	return &Service{
		storage: storage,
		cfg:     cfg,
	}
}

// SetRefreshCache устанавливает кэш refresh-токенов (опционально).
func (s *Service) SetRefreshCache(c cache.RefreshCache) {
	s.rcache = c
}
