package postgres

import (
	"auth-service/internal/models"
	"auth-service/internal/storage"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SaveUser сохраняет нового пользователя в таблице users.
//
// Контракт:
//   - При нарушении уникальности email возвращает storage.ErrAlreadyExists.
//   - Поля user должны быть валидированы на верхнем уровне (ID, Email, PasswordHash, таймстемпы).
func (s *Storage) SaveUser(ctx context.Context, user *models.User) error {
	const op = "storage.postgres.SaveUser"

	query := `
		INSERT INTO users(id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := s.db.Exec(ctx, query,
		user.ID,
		user.Email,
		user.PasswordHash,
		user.CreatedAt,
		user.UpdatedAt,
	)

	if err != nil {
		// маппинг конфликтов уникальности (CITEXT UNIQUE по email).
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return fmt.Errorf("%s: %w", op, storage.ErrAlreadyExists)
		}

		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// UserByEmail возвращает пользователя по email.
//
// Контракт:
//   - Возвращает storage.ErrNotFound, если записи нет.
//   - Столбец email имеет тип CITEXT, поиск регистронезависим.
func (s *Storage) UserByEmail(ctx context.Context, email string) (*models.User, error) {
	const op = "storage.postgres.UserByEmail"

	query := `
		SELECT id, email, password_hash, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var user models.User
	err := s.db.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &user, nil
}

// UserByID возвращает пользователя по идентификатору.
//
// Контракт:
//   - Возвращает storage.ErrNotFound, если записи нет.
func (s *Storage) UserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	const op = "storage.postgres.UserByID"

	query := `
		SELECT id, email, password_hash, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var user models.User
	err := s.db.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFound)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &user, nil
}
