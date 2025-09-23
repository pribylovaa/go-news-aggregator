package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/pribylovaa/go-news-aggregator/pkg/log"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/users-service/internal/storage"
)

// Входные структуры сервисного слоя.
type CreateProfileInput struct {
	UserID   uuid.UUID
	Username string
	Age      uint32
	Country  string
	Gender   models.Gender
}

type UpdateProfileInput struct {
	UserID   uuid.UUID
	Username *string
	Age      *uint32
	Country  *string
	Gender   *models.Gender
	// Mask — список обновляемых полей. Поддерживаются:
	// "username", "age", "country", "gender".
	// Если пусто — обновятся только поля, для которых заданы указатели,
	// иначе — для каждого поля из mask указатель обязателен (иначе ErrInvalidArgument).
	Mask []string
}

type AvatarUploadURLInput struct {
	UserID        uuid.UUID
	ContentType   string
	ContentLength int64
}

type ConfirmAvatarUploadInput struct {
	UserID    uuid.UUID
	AvatarKey string
}

// ProfileByID возвращает профиль по идентификатору пользователя.
//
// Валидация:
//   - userID не должен быть нулевым (uuid.Nil) — иначе ErrInvalidArgument.
//
// Поведение:
//   - при отсутствии записи возвращает ErrNotFound;
//   - ошибки стораджа/БД/контекста маппятся в ErrInternal.
func (s *Service) ProfileByID(ctx context.Context, userID uuid.UUID) (*models.Profile, error) {
	const op = "service/users/ProfileByID"

	lg := log.From(ctx).With("op", op, "user_id", userID.String())

	if userID == uuid.Nil {
		lg.Warn("invalid argument: empty user_id")

		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	result, err := s.profilesStorage.ProfileByID(ctx, userID)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFoundProfile):
			lg.Warn("profile not found")

			return nil, fmt.Errorf("%s: %w", op, ErrNotFound)
		default:
			lg.Error("storage error on ProfileByID", "err", err)

			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return result, nil
}

// CreateProfile создаёт новый профиль пользователя.
//
// Валидация:
//   - userID обязателен (uuid.Nil -> ErrInvalidArgument);
//   - username нормализуется (TrimSpace) и не должен быть пустым;
//   - gender должен входить в допустимый диапазон [GenderUnspecified..GenderOther].
//
// Поведение:
//   - при конфликте уникальности возвращает ErrAlreadyExists;
//   - иные ошибки стораджа маппятся в ErrInternal.
//
// Возвращает:
//   - заполненную доменную модель с серверными полями (timestamps) из БД.
func (s *Service) CreateProfile(ctx context.Context, input CreateProfileInput) (*models.Profile, error) {
	const op = "service/users/CreateProfile"
	lg := log.From(ctx).With("op", op, "user_id", input.UserID.String())

	if input.UserID == uuid.Nil {
		lg.Warn("invalid argument: empty user_id")

		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	input.Username = strings.TrimSpace(input.Username)

	if input.Username == "" {
		lg.Warn("invalid argument: empty username")

		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	if input.Gender < models.GenderUnspecified || input.Gender > models.GenderOther {
		lg.Warn("invalid argument: gender out of range", "gender", input.Gender)

		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	profile := &models.Profile{
		UserID:   input.UserID,
		Username: input.Username,
		Age:      input.Age,
		Country:  strings.TrimSpace(input.Country),
		Gender:   input.Gender,
	}

	result, err := s.profilesStorage.CreateProfile(ctx, profile)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrAlreadyExists):
			lg.Warn("profile already exists")

			return nil, fmt.Errorf("%s: %w", op, ErrAlreadyExists)
		default:
			lg.Error("storage error", "err", err)

			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return result, nil
}

// UpdateProfile выполняет частичное обновление полей профиля.
//
// Маска/правила:
//   - поддерживаются пути: "username", "age", "country", "gender";
//   - если mask пуст — обновляются все поля, для которых переданы непустые указатели;
//   - если mask непуст — для каждого поля из mask указатель обязателен, иначе ErrInvalidArgument;
//   - username при обновлении также нормализуется и не может быть пустым (TrimSpace == "").
//
// Поведение:
//   - no-op (пустой апдейт) допустим — updated_at всё равно увеличится на уровне БД;
//   - при отсутствии записи возвращает ErrNotFound;
//   - все прочие ошибки стораджа маппятся в ErrInternal.
func (s *Service) UpdateProfile(ctx context.Context, input UpdateProfileInput) (*models.Profile, error) {
	const op = "service/users/UpdateProfile"

	lg := log.From(ctx).With("op", op, "user_id", input.UserID.String())

	if input.UserID == uuid.Nil {
		lg.Warn("invalid argument: empty user_id")

		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	allowed := map[string]struct{}{
		"username": {},
		"age":      {},
		"country":  {},
		"gender":   {},
	}

	for _, f := range input.Mask {
		if _, ok := allowed[f]; !ok {
			lg.Warn("invalid mask field", "field", f)

			return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
		}
	}

	useField := func(name string) bool {
		if len(input.Mask) == 0 {
			return true
		}

		for _, f := range input.Mask {
			if f == name {
				return true
			}
		}

		return false
	}

	upd := storage.ProfileUpdate{}

	// username.
	if useField("username") {
		if input.Username != nil {
			val := strings.TrimSpace(*input.Username)

			if val == "" {
				lg.Warn("invalid argument: empty username in update")

				return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
			}

			upd.Username = &val
		} else if len(input.Mask) > 0 {
			lg.Warn("mask requires username but value is nil")

			return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
		}
	}

	// age.
	if useField("age") {
		if input.Age != nil {
			upd.Age = input.Age
		} else if len(input.Mask) > 0 {
			lg.Warn("mask requires age but value is nil")

			return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
		}
	}

	// country (пустая строка допустима — это явное «очистить»).
	if useField("country") {
		if input.Country != nil {
			val := strings.TrimSpace(*input.Country)
			upd.Country = &val
		} else if len(input.Mask) > 0 {
			lg.Warn("mask requires country but value is nil")

			return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
		}
	}

	// gender
	if useField("gender") {
		if input.Gender != nil {
			if *input.Gender < models.GenderUnspecified || *input.Gender > models.GenderOther {
				lg.Warn("invalid argument: gender out of range", "gender", *input.Gender)

				return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
			}

			upd.Gender = input.Gender
		} else if len(input.Mask) > 0 {
			lg.Warn("mask requires gender but value is nil")

			return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
		}
	}

	result, err := s.profilesStorage.UpdateProfile(ctx, input.UserID, upd)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFoundProfile):
			lg.Warn("profile not found")

			return nil, fmt.Errorf("%s: %w", op, ErrNotFound)
		default:
			lg.Error("storage error", "err", err)

			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return result, nil
}

// AvatarUploadURL генерирует presigned PUT URL для загрузки аватара в S3/MinIO.
//
// Валидация:
//   - userID обязателен; contentType не пустой; contentLength > 0;
//   - дополнительные ограничения (тип/размер) проверяет слой storage.Avatars.
//
// Поведение:
//   - на ошибки валидации в сторадже возвращает ErrInvalidArgument;
//   - на иные ошибки (проблемы S3/клиента) — ErrInternal.
//
// Возвращает:
//   - UploadInfo с конечной URL, будущим ключом объекта, временем жизни подписи
//     и обязательными заголовками для PUT (должны быть переданы клиентом).
func (s *Service) AvatarUploadURL(ctx context.Context, input AvatarUploadURLInput) (*storage.UploadInfo, error) {
	const op = "service/users/AvatarUploadURL"

	lg := log.From(ctx).With("op", op, "user_id", input.UserID.String())

	if input.UserID == uuid.Nil || strings.TrimSpace(input.ContentType) == "" || input.ContentLength <= 0 {
		lg.Warn("invalid argument for presign", "content_type", input.ContentType, "content_length", input.ContentLength)

		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	result, err := s.avatarsStorage.AvatarUploadURL(ctx, input.UserID, input.ContentType, input.ContentLength)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrInvalidArgument):
			lg.Warn("validation failed in storage", "err", err)

			return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
		default:
			lg.Error("storage error", "err", err)

			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return result, nil
}

// ConfirmAvatarUpload подтверждает успешную загрузку аватара и фиксирует атрибуты в профиле.
//
// Процесс:
//  1. в storage.AvatarsStorage проверяется ключ (принадлежность userID, наличие),
//     соответствие ограничениям типа/размера; при успехе формируется публичный URL (если сконфигурирован);
//  2. в storage.ProfilesStorage выполняется апдейт записи профиля (avatar_key/url + updated_at).
//
// Валидация:
//   - userID обязателен; avatarKey не пуст.
//
// Поведение/ошибки:
//   - ErrInvalidArgument — неверный ключ/нарушены ограничения;
//   - ErrNotFound — объект в бакете не найден или профиль отсутствует;
//   - ErrInternal — прочие ошибки стораджа/S3.
func (s *Service) ConfirmAvatarUpload(ctx context.Context, input ConfirmAvatarUploadInput) (*models.Profile, error) {
	const op = "service/users/ConfirmAvatarUpload"

	lg := log.From(ctx).With("op", op, "user_id", input.UserID.String(), "avatar_key", input.AvatarKey)

	if input.UserID == uuid.Nil || strings.TrimSpace(input.AvatarKey) == "" {
		lg.Warn("invalid argument for confirm")

		return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
	}

	publicURL, err := s.avatarsStorage.CheckAvatarUpload(ctx, input.UserID, input.AvatarKey)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrInvalidArgument):
			lg.Warn("invalid avatar key or attributes", "err", err)

			return nil, fmt.Errorf("%s: %w", op, ErrInvalidArgument)
		case errors.Is(err, storage.ErrNotFoundAvatar):
			lg.Warn("avatar object not found")

			return nil, fmt.Errorf("%s: %w", op, ErrNotFound)
		default:
			lg.Error("storage error on CheckAvatarUpload", "err", err)

			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	result, err := s.profilesStorage.ConfirmAvatarUpload(ctx, input.UserID, input.AvatarKey, publicURL)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrNotFoundProfile):
			lg.Warn("profile not found on confirm")

			return nil, fmt.Errorf("%s: %w", op, ErrNotFound)
		default:
			lg.Error("storage error on ConfirmAvatarUpload", "err", err)

			return nil, fmt.Errorf("%s: %w", op, ErrInternal)
		}
	}

	return result, nil
}
