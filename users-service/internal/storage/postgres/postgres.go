// postgres предоставляет реализацию storage.ProfilesStorage на базе PostgreSQL.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
)

type ProfilesStorage struct {
	db *pgxpool.Pool
}

// New создает и инициализирует пул соединений к PostgreSQL.
func New(ctx context.Context, dbURL string) (*ProfilesStorage, error) {
	const op = "storage/postgres/New"

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

	return &ProfilesStorage{db: db}, nil
}

// Close закрывает пул соединений.
// Должен вызываться при остановке приложения.
func (s *ProfilesStorage) Close() {
	s.db.Close()
}

// Проверка выполнения контракта верхнего уровня.
var _ storage.ProfilesStorage = (*ProfilesStorage)(nil)
