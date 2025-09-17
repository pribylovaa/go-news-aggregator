// Файл token.go инкапсулирует логику выпуска и проверки токенов:
//   - Access JWT (HS256) с claim’ами uid/email и стандартными полями (iss/sub/aud/iat/exp);
//   - Refresh-токен как случайная 256-битная строка (base64url), на сервере хранится только SHA-256 хэш.
//
// Безопасность:
//   - Секрет подписи берется из конфигурации (см. config.AuthConfig) и должен быть криптографически стойким;
//   - Для JWT строго проверяются алгоритм, issuer, audience, срок действия (с 5s leeway);
//   - Refresh-токены отзываются и ротируются; истечение проверяется по правилу ExpiresAt <= now (UTC).
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/auth-service/internal/storage"
	"github.com/pribylovaa/go-news-aggregator/pkg/log"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// accessClaims — частный тип claim’ов access JWT.
// Включает UserID (uuid) и Email, плюс стандартные RegisteredClaims.
type accessClaims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// generateAccessToken выпускает подписанный HS256 JWT для пользователя userID/email.
// Контракт:
//   - Включает iss/sub/aud/iat/exp; audience берется из конфигурации;
//   - На ошибке подписи возвращает обёрнутую ошибку; секрет берётся из cfg.JWTSecret;
//   - now должен быть в UTC; exp = now + AccessTokenTTL.
func (s *Service) generateAccessToken(ctx context.Context, userID uuid.UUID, email string, now time.Time) (string, error) {
	const op = "service.token.generateAccessToken"

	lg := log.From(ctx)

	claims := accessClaims{
		UserID: userID.String(),
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    s.cfg.Issuer,
			Subject:   userID.String(),
			Audience:  jwt.ClaimStrings(s.cfg.Audience),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		lg.Error("access_token_sign_failed",
			slog.String("op", op),
			slog.String("err", err.Error()),
		)
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return signed, nil
}

// validateAccessToken проверяет access JWT и возвращает (userID, email) при успехе.
// Проверяется: подпись HS256, алгоритм, issuer, audience, срок действия (leeway 5s).
// Ошибки:
//   - ErrTokenExpired — если истёк срок действия;
//   - ErrInvalidToken — при любых иных нарушениях формата/подписи/клеймов.
func (s *Service) validateAccessToken(tokenStr string) (uuid.UUID, string, error) {
	const op = "service.token.validateAccessToken"

	token, err := jwt.ParseWithClaims(tokenStr, &accessClaims{},
		func(t *jwt.Token) (interface{}, error) {
			if t.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("%s: %w", op, ErrInvalidToken)
			}

			return []byte(s.cfg.JWTSecret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithLeeway(5*time.Second),
		jwt.WithIssuer(s.cfg.Issuer),
		jwt.WithAudience(s.cfg.Audience...),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return uuid.Nil, "", fmt.Errorf("%s: %w", op, ErrTokenExpired)
		}

		return uuid.Nil, "", fmt.Errorf("%s: %w", op, ErrInvalidToken)
	}

	claims, ok := token.Claims.(*accessClaims)
	if !ok || !token.Valid {
		return uuid.Nil, "", fmt.Errorf("%s: %w", op, ErrInvalidToken)
	}

	uid, err := uuid.Parse(claims.UserID)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("%s: %w", op, ErrInvalidToken)
	}

	return uid, claims.Email, nil
}

// generateRefreshToken создаёт новый refresh-токен для userID, сохраняет его хэш и возвращает плейн-строку.
// Реализация:
//   - Порождает 32 случайных байта (crypto/rand), кодирует в base64.RawURLEncoding;
//   - Хэширует SHA-256 → base64.RawURLEncoding и сохраняет через Storage.SaveRefreshToken;
//   - Повторяет попытку при конфликте уникальности (редкая коллизия) до 5 раз.
//
// Ошибки: ErrRefreshTokenCollision — если превышен лимит ретраев.
func (s *Service) generateRefreshToken(ctx context.Context, userID uuid.UUID) (string, error) {
	const (
		op          = "service.token.generateRefreshToken"
		maxAttempts = 5
	)

	lg := log.From(ctx)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			lg.Error("refresh_rand_failed",
				slog.String("op", op),
				slog.String("err", err.Error()),
			)
			return "", fmt.Errorf("%s: %w", op, err)
		}
		plain := base64.RawURLEncoding.EncodeToString(b)

		hashBytes := sha256.Sum256([]byte(plain))
		hash := base64.RawURLEncoding.EncodeToString(hashBytes[:])

		now := time.Now().UTC()
		token := &models.RefreshToken{
			RefreshTokenHash: hash,
			UserID:           userID,
			CreatedAt:        now,
			ExpiresAt:        now.Add(s.cfg.RefreshTokenTTL),
			Revoked:          false,
		}

		if err := s.storage.SaveRefreshToken(ctx, token); err != nil {
			if errors.Is(err, storage.ErrAlreadyExists) {
				// Редкая коллизия — пробуем сгенерировать заново.
				continue
			}

			lg.Error("save_refresh_token_failed",
				slog.String("op", op),
				slog.String("err", err.Error()),
			)
			return "", fmt.Errorf("%s: %w", op, err)
		}

		return plain, nil
	}

	lg.Error("refresh_collision_exceeded",
		slog.String("op", op),
	)

	return "", fmt.Errorf("%s: %w", op, ErrRefreshTokenCollision)
}

// validateRefreshToken валидирует плейн refresh-токен, возвращая запись из хранилища.
// Проверки:
//   - наличие записи (иначе ErrInvalidToken);
//   - revoked (ErrTokenRevoked);
//   - истечение по правилу ExpiresAt <= now (UTC) (ErrTokenExpired).
func (s *Service) validateRefreshToken(ctx context.Context, plain string) (*models.RefreshToken, error) {
	const op = "service.token.validateRefreshToken"

	lg := log.From(ctx)

	hashBytes := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(hashBytes[:])

	token, err := s.storage.RefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			lg.Warn("refresh_lookup_not_found",
				slog.String("op", op),
			)
			return nil, fmt.Errorf("%s: %w", op, ErrInvalidToken)
		}

		lg.Error("refresh_lookup_failed",
			slog.String("op", op),
			slog.String("err", err.Error()),
		)
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if token.Revoked {
		lg.Warn("refresh_revoked",
			slog.String("op", op),
			slog.String("user_id", token.UserID.String()),
		)
		return nil, fmt.Errorf("%s: %w", op, ErrTokenRevoked)
	}

	now := time.Now().UTC()
	if !token.ExpiresAt.After(now) {
		lg.Warn("refresh_expired",
			slog.String("op", op),
			slog.String("user_id", token.UserID.String()),
		)
		return nil, fmt.Errorf("%s: %w", op, ErrTokenExpired)
	}

	return token, nil
}
