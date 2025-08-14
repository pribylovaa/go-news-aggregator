package postgres

import (
	"auth-service/internal/storage"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	db *pgxpool.Pool
}

// New создает новое подключение к PostgreSQL.
func New(ctx context.Context, dbURL string) (*Storage, error) {
	const op = "storage.postgres.New"

	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &Storage{db: db}, nil
}

// Close закрывает пул соединений.
func (s *Storage) Close() {
	s.db.Close()
}

// Проверка на соответствие интерфейсу Storage.
var _ storage.Storage = (*Storage)(nil)
