// models содержит доменные сущности news-сервиса.
// Эти типы используются слоями бизнес-логики, хранилища и транспорта.
package models

import (
	"time"

	"github.com/google/uuid"
)

// News — доменная сущность новости.
//
// Особенности:
//   - ID — UUIDv4;
//   - Временные метки — в UTC.
type News struct {
	// ID — уникальный идентификатор новости.
	ID uuid.UUID
	// Title - название новости.
	Title string
	// Category - категория новости.
	Category string
	// ShortDescription - тизер новости.
	ShortDescription string
	// LongDescription - полный текст новости.
	LongDescription string
	// Link - ссылка на источник.
	Link string
	// ImageURL - ссылка на обложку к новости.
	ImageURL string
	// PublishedAt - время публикации новости у источника.
	PublishedAt time.Time
	// FetchedAt - время загрузки новости в БД (UTC).
	FetchedAt time.Time
}

// ListOptions — параметры выборки списков доменных сущностей.
//
// Особенности:
//   - при Limit == 0 применяется серверный default (из config.LimitsConfig.Default);
//   - PageToken == "" -> первая страница
type ListOptions struct {
	Limit     int32
	PageToken string
}

// Page — страница результатов со ссылкой на продолжение.
type Page struct {
	Items         []News
	NextPageToken string
}
