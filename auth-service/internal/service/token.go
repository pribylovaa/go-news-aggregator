package service

import (
	"auth-service/internal/models"
	"auth-service/internal/storage"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
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
func (s *Service) generateAccessToken(userID uuid.UUID, email string) (string, error) {
	claims := accessClaims{
		UserID: userID.String(),
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(s.cfg.AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

// validateAccessToken валидирует access-токен.
func (s *Service) validateAccessToken(tokenStr string) (uuid.UUID, string, error) {
	const op = "service.token.validateAccessToken"

	token, err := jwt.ParseWithClaims(tokenStr, &accessClaims{},
		func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("%s: %w", op, ErrInvalidToken)
			}

			return []byte(s.cfg.JWTSecret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
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
func (s *Service) generateRefreshToken(ctx context.Context, userID uuid.UUID) (*models.RefreshToken, string, error) {
	const (
		op          = "service.token.generateRefreshToken"
		maxAttempts = 5
	)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, "", fmt.Errorf("%s: %w", op, err)
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

			return nil, "", fmt.Errorf("%s: %w", op, err)
		}

		return token, plain, nil
	}

	return nil, "", fmt.Errorf("%s: %w", op, ErrRefreshTokenCollision)
}

// validateRefreshToken валидирует refresh-токен.
func (s *Service) validateRefreshToken(ctx context.Context, plain string) (*models.RefreshToken, error) {
	const op = "service.token.validateRefreshToken"

	hashBytes := sha256.Sum256([]byte(plain))
	hash := base64.RawURLEncoding.EncodeToString(hashBytes[:])

	token, err := s.storage.RefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("%s: %w", op, ErrInvalidToken)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if token.Revoked {
		return nil, fmt.Errorf("%s: %w", op, ErrTokenRevoked)
	}

	if time.Now().UTC().After(token.ExpiresAt) {
		return nil, fmt.Errorf("%s: %w", op, ErrTokenExpired)
	}

	return token, nil
}
