// models содержит доменные сущности users-сервиса.
// Эти типы используются слоями бизнес-логики, хранилища и транспорта.
package models

import (
	"time"

	"github.com/google/uuid"
)

// Gender — внутренний enum;
type Gender int8

const (
	GenderUnspecified Gender = iota
	GenderMale
	GenderFemale
	GenderOther
)

func (g Gender) String() string {
	switch g {
	case GenderMale:
		return "male"
	case GenderFemale:
		return "female"
	case GenderOther:
		return "other"
	default:
		return "unspecified"
	}
}

// Profile — внутренняя доменная модель.
// CreatedAt/UpdateAt - наружу/внутрь gRPC конвертируем в int64.
type Profile struct {
	UserID    uuid.UUID
	Username  string
	Age       uint32
	Country   string
	Gender    Gender
	AvatarKey string
	AvatarURL string
	CreatedAt time.Time
	UpdatedAt time.Time
}
