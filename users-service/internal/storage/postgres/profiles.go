package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
)

// profileColumns — единый список колонок таблицы profiles,
// используемый в SELECT/RETURNING, чтобы гарантировать одинаковый порядок сканирования.
const profileColumns = `
user_id, username, age, country, gender, avatar_key, avatar_url, created_at, updated_at
`

// scanProfile сканирует одну строку профиля из результата запроса
// в доменную модель с корректными кастами типов (INT -> uint32, SMALLINT -> models.Gender).
func scanProfile(row pgx.Row) (*models.Profile, error) {
	var profile models.Profile
	var age int32
	var gender int16

	if err := row.Scan(
		&profile.UserID,
		&profile.Username,
		&age,
		&profile.Country,
		&gender,
		&profile.AvatarKey,
		&profile.AvatarURL,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	); err != nil {
		return nil, err
	}

	if age < 0 {
		age = 0
	}
	profile.Age = uint32(age)

	profile.Gender = models.Gender(gender)

	return &profile, nil
}

// CreateProfile вставляет новую запись профиля.
// Ошибки: storage.ErrAlreadyExists при конфликте уникальности (PK/UNIQUE), иные — как есть.
func (s *ProfilesStorage) CreateProfile(ctx context.Context, profile *models.Profile) (*models.Profile, error) {
	const op = "storage/postgres/profiles/CreateProfile"

	q := `
	INSERT INTO profiles (user_id, username, age, country, gender, avatar_key, avatar_url)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING 
	` + profileColumns

	row := s.db.QueryRow(ctx, q,
		profile.UserID,
		profile.Username,
		int32(profile.Age),
		profile.Country,
		int16(profile.Gender),
		profile.AvatarKey,
		profile.AvatarURL,
	)

	result, err := scanProfile(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrAlreadyExists)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return result, nil
}

// ProfileByID возвращает профиль по user_id.
// Ошибки: storage.ErrNotFoundProfile, либо ошибка выполнения запроса.
func (s *ProfilesStorage) ProfileByID(ctx context.Context, userID uuid.UUID) (*models.Profile, error) {
	const op = "storage/postgres/profiles/ProfileByID"

	q := `SELECT ` + profileColumns + ` FROM profiles WHERE user_id = $1`

	row := s.db.QueryRow(ctx, q, userID)

	result, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFoundProfile)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return result, nil
}

// UpdateProfile выполняет частичный апдейт: обновляет только поля,
// указанные непустыми pointer-полями, и всегда сдвигает updated_at = now().
// Ошибки: storage.ErrNotFoundProfile при отсутствии записи.
func (s *ProfilesStorage) UpdateProfile(ctx context.Context, userID uuid.UUID, update storage.ProfileUpdate) (*models.Profile, error) {
	const op = "storage/postgres/profiles/UpdateProfile"

	sets := []string{"updated_at = now()"}
	args := make([]any, 0, 5)
	count := 1

	if update.Username != nil {
		count++
		sets = append(sets, fmt.Sprintf("username = $%d", count))
		args = append(args, *update.Username)
	}

	if update.Age != nil {
		count++
		sets = append(sets, fmt.Sprintf("age = $%d", count))
		args = append(args, int32(*update.Age))
	}

	if update.Country != nil {
		count++
		sets = append(sets, fmt.Sprintf("country = $%d", count))
		args = append(args, *update.Country)
	}

	if update.Gender != nil {
		count++
		sets = append(sets, fmt.Sprintf("gender = $%d", count))
		args = append(args, int16(*update.Gender))
	}

	count++
	args = append(args, userID)

	q := fmt.Sprintf(`UPDATE profiles SET %s WHERE user_id = $%d RETURNING %s`,
		strings.Join(sets, ", "), count, profileColumns)

	row := s.db.QueryRow(ctx, q, args...)

	result, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFoundProfile)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return result, nil
}

// ConfirmAvatarUpload фиксирует avatar_key и (опционально) avatar_url
// после успешной проверки объекта в S3/MinIO. Всегда обновляет updated_at.
// Ошибки: storage.ErrNotFoundProfile при отсутствии записи.
func (s *ProfilesStorage) ConfirmAvatarUpload(ctx context.Context, userID uuid.UUID, key, publicURL string) (*models.Profile, error) {
	const op = "storage/postgres/profiles/ConfirmAvatarUpload"

	q := `
	UPDATE profiles 
	SET avatar_key = $2, avatar_url = $3, updated_at = now()
	WHERE user_id = $1
	RETURNING
	` + profileColumns

	row := s.db.QueryRow(ctx, q, userID, key, publicURL)

	result, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%s: %w", op, storage.ErrNotFoundProfile)
		}

		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return result, nil
}
