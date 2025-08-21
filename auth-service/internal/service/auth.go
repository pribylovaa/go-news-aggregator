package service

import (
	"auth-service/internal/models"
	"auth-service/internal/storage"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// RegisterUser регистрирует нового пользователя.
func (s *Service) RegisterUser(ctx context.Context, email, password string) (*models.TokenPair, uuid.UUID, error) {
	const op = "service.auth.RegisterUser"

	normEmail, err := validateEmail(email)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidEmail)
	}

	if err := validatePassword(password); err != nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	_, err = s.storage.UserByEmail(ctx, normEmail)
	if err == nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrEmailTaken)
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	hashedPassword, err := hashPassword(password)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	user := &models.User{
		ID:           uuid.New(),
		Email:        normEmail,
		PasswordHash: hashedPassword,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := s.storage.SaveUser(ctx, user); err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrEmailTaken)
		}

		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	return s.issueTokenPair(ctx, user, "")
}

// LoginUser выполняет вход по email+пароль.
func (s *Service) LoginUser(ctx context.Context, email, password string) (*models.TokenPair, uuid.UUID, error) {
	const op = "service.auth.LoginUser"

	normEmail, err := validateEmail(email)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	if len(password) == 0 {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	user, err := s.storage.UserByEmail(ctx, normEmail)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}

		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	if !checkPassword(user.PasswordHash, password) {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	return s.issueTokenPair(ctx, user, "")
}

// RefreshToken обновляет пару токенов по refresh-токену.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*models.TokenPair, uuid.UUID, error) {
	const op = "service.auth.RefreshToken"

	token, err := s.validateRefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	user, err := s.storage.UserByID(ctx, token.UserID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	hashBytes := sha256.Sum256([]byte(refreshToken))
	hash := base64.RawURLEncoding.EncodeToString(hashBytes[:])

	return s.issueTokenPair(ctx, user, hash)
}

// RevokeToken отзывает refresh-токен.
func (s *Service) RevokeToken(ctx context.Context, refreshToken string) error {
	const op = "service.auth.RevokeToken"

	hashBytes := sha256.Sum256([]byte(refreshToken))
	hash := base64.RawURLEncoding.EncodeToString(hashBytes[:])

	revoked, err := s.storage.RevokeRefreshToken(ctx, hash)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("%s: %w", op, ErrInvalidToken)
		}

		return fmt.Errorf("%s: %w", op, err)
	}

	if !revoked {
		return fmt.Errorf("%s: %w", op, ErrTokenRevoked)
	}

	return nil
}

// ValidateToken проверяет access-токен и возвращает данные пользователя.
func (s *Service) ValidateToken(ctx context.Context, accessToken string) (uuid.UUID, string, error) {
	const op = "service.auth.ValidateToken"

	uid, email, err := s.validateAccessToken(accessToken)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("%s: %w", op, err)
	}

	return uid, email, nil
}

// hashPassword хэширует пароль с помощью bcrypt.
func hashPassword(password string) (string, error) {
	const op = "service.auth.hashPassword"

	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return string(bytes), nil
}

// checkPassword сравнивает пароль с хэшем.
func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// validateEmail проверяет базовый формат email и обрезает пробелы снаружи.
func validateEmail(raw string) (string, error) {
	const op = "service.auth.validateEmail"

	email := strings.TrimSpace(raw)
	if email == "" {
		return "", fmt.Errorf("%s: %w", op, ErrInvalidEmail)
	}

	if _, err := mail.ParseAddress(email); err != nil {
		return "", fmt.Errorf("%s: %w", op, ErrInvalidEmail)
	}

	return strings.ToLower(email), nil
}

// validatePassword проверяет минимальные требования к паролю.
// Политика по умолчанию: длина >= 8, хотя бы одна строчная, заглавная, цифра и спецсимвол.
func validatePassword(pw string) error {
	const op = "service.auth.validatePassword"

	if len(pw) == 0 {
		return fmt.Errorf("%s: %w", op, ErrEmptyPassword)
	}

	if len([]rune(pw)) < 8 {
		return fmt.Errorf("%s: %w", op, ErrWeakPassword)
	}

	var hasLower, hasUpper, hasDigit, hasSpecial bool
	for _, r := range pw {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	if !(hasLower && hasUpper && hasDigit && hasSpecial) {
		return fmt.Errorf("%s: %w", op, ErrWeakPassword)
	}

	return nil
}

// issueTokenPair выпускает новую пару access+refresh токенов.
// Если oldRefreshHash != "", пытается атомарно отозвать старый refresh-токен.
func (s *Service) issueTokenPair(ctx context.Context, user *models.User, oldRefreshHash string) (*models.TokenPair, uuid.UUID, error) {
	const op = "service.auth.issueTokenPair"

	now := time.Now().UTC()

	accessToken, err := s.generateAccessToken(user.ID, user.Email, now)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	if oldRefreshHash != "" {
		revoked, err := s.storage.RevokeRefreshToken(ctx, oldRefreshHash)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidToken)
			}

			return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
		}

		if !revoked {
			return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrTokenRevoked)
		}
	}

	plain, err := s.generateRefreshToken(ctx, user.ID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	return &models.TokenPair{
		AccessToken:     accessToken,
		RefreshToken:    plain,
		AccessExpiresAt: now.Add(s.cfg.AccessTokenTTL),
	}, user.ID, nil
}
