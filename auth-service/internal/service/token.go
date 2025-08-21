package service

import (
	"auth-service/internal/models"
	"auth-service/internal/pkg/log"
	"auth-service/internal/storage"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type accessClaims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// generateAccessToken генерирует access-токен.
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

// validateAccessToken валидирует access-токен.
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

// generateRefreshToken создает новый refresh-токен.
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

// validateRefreshToken валидирует refresh-токен.
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

	if time.Now().UTC().After(token.ExpiresAt) {
		lg.Warn("refresh_expired",
			slog.String("op", op),
			slog.String("user_id", token.UserID.String()),
		)
		return nil, fmt.Errorf("%s: %w", op, ErrTokenExpired)
	}

	return token, nil
}
