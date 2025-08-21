package service

import (
	"auth-service/internal/models"
	"auth-service/internal/pkg/log"
	"auth-service/internal/pkg/redact"
	"auth-service/internal/storage"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
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

	lg := log.From(ctx)
	lg.Info("register_attempt",
		slog.String("op", op),
		slog.String("email", redact.Email(email)),
	)

	normEmail, err := validateEmail(email)
	if err != nil {
		lg.Warn("register_validation_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(email)),
			slog.String("reason", "invalid_email"),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidEmail)
	}

	if err := validatePassword(password); err != nil {
		lg.Warn("register_validation_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(normEmail)),
			slog.String("reason", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	_, err = s.storage.UserByEmail(ctx, normEmail)
	if err == nil {
		lg.Warn("register_email_taken",
			slog.String("op", op),
			slog.String("email", redact.Email(normEmail)),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrEmailTaken)
	}

	if !errors.Is(err, storage.ErrNotFound) {
		lg.Error("register_lookup_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(normEmail)),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	hashedPassword, err := hashPassword(password)
	if err != nil {
		lg.Error("hash_password_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(normEmail)),
			slog.String("err", err.Error()),
		)
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
			lg.Warn("register_email_taken",
				slog.String("op", op),
				slog.String("email", redact.Email(normEmail)),
			)
			return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrEmailTaken)
		}

		lg.Error("save_user_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(normEmail)),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	lg.Info("user_created",
		slog.String("op", op),
		slog.String("user_id", user.ID.String()),
		slog.String("email", redact.Email(user.Email)),
	)

	tokenPair, uid, err := s.issueTokenPair(ctx, user, "")
	if err != nil {
		lg.Error("issue_token_pair_failed",
			slog.String("op", op),
			slog.String("user_id", user.ID.String()),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	lg.Info("register_ok",
		slog.String("op", op),
		slog.String("user_id", uid.String()),
	)

	return tokenPair, uid, nil
}

// LoginUser выполняет вход по email+пароль.
func (s *Service) LoginUser(ctx context.Context, email, password string) (*models.TokenPair, uuid.UUID, error) {
	const op = "service.auth.LoginUser"

	lg := log.From(ctx)
	lg.Info("login_attempt",
		slog.String("op", op),
		slog.String("email", redact.Email(email)),
	)

	normEmail, err := validateEmail(email)
	if err != nil {
		lg.Warn("login_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(email)),
			slog.String("reason", "invalid_email"),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	if len(password) == 0 {
		lg.Warn("login_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(normEmail)),
			slog.String("reason", "empty_password"),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	user, err := s.storage.UserByEmail(ctx, normEmail)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			lg.Warn("login_failed",
				slog.String("op", op),
				slog.String("email", redact.Email(normEmail)),
				slog.String("reason", "user_not_found"),
			)
			return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
		}

		lg.Error("login_lookup_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(normEmail)),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	if !checkPassword(user.PasswordHash, password) {
		lg.Warn("login_failed",
			slog.String("op", op),
			slog.String("email", redact.Email(normEmail)),
			slog.String("reason", "wrong_password"),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidCredentials)
	}

	tokenPair, uid, err := s.issueTokenPair(ctx, user, "")
	if err != nil {
		lg.Error("issue_token_pair_failed",
			slog.String("op", op),
			slog.String("user_id", user.ID.String()),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	lg.Info("login_ok",
		slog.String("op", op),
		slog.String("user_id", uid.String()),
	)

	return tokenPair, uid, nil
}

// RefreshToken обновляет пару токенов по refresh-токену.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*models.TokenPair, uuid.UUID, error) {
	const op = "service.auth.RefreshToken"

	lg := log.From(ctx)

	token, err := s.validateRefreshToken(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrTokenExpired) || errors.Is(err, ErrTokenRevoked) {
			lg.Warn("refresh_invalid",
				slog.String("op", op),
				slog.String("reason", err.Error()),
			)
		} else {
			lg.Error("refresh_lookup_failed",
				slog.String("op", op),
				slog.String("err", err.Error()),
			)
		}
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	user, err := s.storage.UserByID(ctx, token.UserID)
	if err != nil {
		lg.Error("refresh_user_lookup_failed",
			slog.String("op", op),
			slog.String("user_id", token.UserID.String()),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	hashBytes := sha256.Sum256([]byte(refreshToken))
	hash := base64.RawURLEncoding.EncodeToString(hashBytes[:])

	tokenPair, uid, err := s.issueTokenPair(ctx, user, hash)
	if err != nil {
		lg.Error("issue_token_pair_failed",
			slog.String("op", op),
			slog.String("user_id", user.ID.String()),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	lg.Info("refresh_ok",
		slog.String("op", op),
		slog.String("user_id", uid.String()),
	)

	return tokenPair, uid, nil
}

// RevokeToken отзывает refresh-токен.
func (s *Service) RevokeToken(ctx context.Context, refreshToken string) error {
	const op = "service.auth.RevokeToken"

	lg := log.From(ctx)

	hashBytes := sha256.Sum256([]byte(refreshToken))
	hash := base64.RawURLEncoding.EncodeToString(hashBytes[:])

	revoked, err := s.storage.RevokeRefreshToken(ctx, hash)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			lg.Warn("revoke_invalid",
				slog.String("op", op),
			)
			return fmt.Errorf("%s: %w", op, ErrInvalidToken)
		}

		lg.Error("revoke_error",
			slog.String("op", op),
			slog.String("err", err.Error()),
		)
		return fmt.Errorf("%s: %w", op, err)
	}

	if !revoked {
		lg.Warn("revoke_already",
			slog.String("op", op),
		)
		return fmt.Errorf("%s: %w", op, ErrTokenRevoked)
	}

	lg.Info("revoke_ok",
		slog.String("op", op),
	)

	return nil
}

// ValidateToken проверяет access-токен и возвращает данные пользователя.
func (s *Service) ValidateToken(ctx context.Context, accessToken string) (uuid.UUID, string, error) {
	const op = "service.auth.ValidateToken"

	lg := log.From(ctx)

	uid, email, err := s.validateAccessToken(accessToken)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrTokenExpired) {
			lg.Warn("validate_failed",
				slog.String("op", op),
				slog.String("reason", err.Error()),
			)
		} else {
			lg.Error("validate_error",
				slog.String("op", op),
				slog.String("err", err.Error()),
			)
		}
		return uuid.Nil, "", fmt.Errorf("%s: %w", op, err)
	}

	lg.Info("validate_ok",
		slog.String("op", op),
		slog.String("user_id", uid.String()),
	)

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

	accessToken, err := s.generateAccessToken(ctx, user.ID, user.Email, now)
	if err != nil {
		log.From(ctx).Error("access_token_generate_failed",
			slog.String("op", op),
			slog.String("user_id", user.ID.String()),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	if oldRefreshHash != "" {
		revoked, err := s.storage.RevokeRefreshToken(ctx, oldRefreshHash)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				log.From(ctx).Warn("rotate_old_refresh_not_found",
					slog.String("op", op),
					slog.String("user_id", user.ID.String()),
				)
				return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrInvalidToken)
			}

			log.From(ctx).Error("rotate_old_refresh_failed",
				slog.String("op", op),
				slog.String("user_id", user.ID.String()),
				slog.String("err", err.Error()),
			)
			return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
		}

		if !revoked {
			log.From(ctx).Warn("rotate_old_refresh_already_revoked",
				slog.String("op", op),
				slog.String("user_id", user.ID.String()),
			)
			return nil, uuid.Nil, fmt.Errorf("%s: %w", op, ErrTokenRevoked)
		}
	}

	plain, err := s.generateRefreshToken(ctx, user.ID)
	if err != nil {
		log.From(ctx).Error("refresh_token_generate_failed",
			slog.String("op", op),
			slog.String("user_id", user.ID.String()),
			slog.String("err", err.Error()),
		)
		return nil, uuid.Nil, fmt.Errorf("%s: %w", op, err)
	}

	return &models.TokenPair{
		AccessToken:     accessToken,
		RefreshToken:    plain,
		AccessExpiresAt: now.Add(s.cfg.AccessTokenTTL),
	}, user.ID, nil
}
