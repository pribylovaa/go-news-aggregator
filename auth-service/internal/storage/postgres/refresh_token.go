package postgres

import (
	"auth-service/internal/models"
	"auth-service/internal/storage"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SaveRefreshToken сохраняет новый refresh-токен в БД.
func (s *Storage) SaveRefreshToken(ctx context.Context, token *models.RefreshToken) error {
	const op = "storage.postgres.SaveRefreshToken"

	query := `
        INSERT INTO refresh_tokens(token_hash, user_id, created_at, expires_at, revoked)
        VALUES ($1, $2, $3, $4, $5)
    `

	_, err := s.db.Exec(ctx, query,
		token.RefreshTokenHash,
		token.UserID,
		token.CreatedAt,
		token.ExpiresAt,
		token.Revoked,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return fmt.Errorf("%s: %w", op, storage.ErrAlreadyExists)
		}

		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// RefreshTokenByHash находит refresh-токен по его хэшу.
func (s *Storage) RefreshTokenByHash(ctx context.Context, hash string) (*models.RefreshToken, error) {
	const op = "storage.postgres.RefreshTokenByHash"

	query := `
        SELECT token_hash, user_id, created_at, expires_at, revoked
        FROM refresh_tokens
        WHERE token_hash = $1
    `

	var token models.RefreshToken
	err := s.db.QueryRow(ctx, query, hash).Scan(
		&token.RefreshTokenHash,
		&token.UserID,
		&token.CreatedAt,
		&token.ExpiresAt,
		&token.Revoked,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &token, nil
}

// RevokeRefreshToken помечает токен как отозванный.
func (s *Storage) RevokeRefreshToken(ctx context.Context, hash string) error {
	const op = "storage.postgres.RevokeRefreshToken"

	query := `
        UPDATE refresh_tokens
        SET revoked = TRUE
        WHERE token_hash = $1
    `

	cmdTag, err := s.db.Exec(ctx, query, hash)

	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%s: %w", op, storage.ErrNotFound)
	}

	return nil
}

// RevokeRefreshTokenIfActive пытается отозвать refresh-токен, если он ещё не был отозван.
// Возвращает:
//
//	(true, nil)  — токен был активен и успешно отозван сейчас;
//	(false, nil) — токен существует, но уже был отозван;
//	(false, ErrNotFound) — токен не найден.
func (s *Storage) RevokeRefreshTokenIfActive(ctx context.Context, hash string) (bool, error) {
	const op = "storage.postgres.RevokeRefreshTokenIfActive"

	const upd = `
		UPDATE refresh_tokens
		SET revoked = TRUE
		WHERE token_hash = $1 AND revoked = FALSE
		RETURNING user_id
	`

	var userID string
	err := s.db.QueryRow(ctx, upd, hash).Scan(&userID)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("%s: %w", op, err)
	}

	const sel = `
		SELECT revoked
		FROM refresh_tokens
		WHERE token_hash = $1
	`

	var revoked bool
	err = s.db.QueryRow(ctx, sel, hash).Scan(&revoked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
		}

		return false, fmt.Errorf("%s: %w", op, err)
	}

	return false, nil
}

// DeleteExpiredTokens удаляет все просроченные токены.
func (s *Storage) DeleteExpiredTokens(ctx context.Context, now time.Time) error {
	const op = "storage.postgres.DeleteExpiredTokens"

	query := `
        DELETE FROM refresh_tokens
        WHERE expires_at <= $1
    `

	_, err := s.db.Exec(ctx, query, now)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
