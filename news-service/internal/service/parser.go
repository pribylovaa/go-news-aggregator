package service

import (
	"context"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
)

// Parser описывает абстракцию источника новостей (RSS/Atom и т.п.),
// который парсит несколько лент и возвращает доменные объекты.
//
// Требования к реализации:
// 1) Поле FetchedAt в возвращаемых items должно быть нулевым —
// его проставляет оркестратор сервиса.
// 2) Link должен быть нормализован (без #fragment, UTM и прочих трекеров)
// для корректной идемпотентности на уровне БД.
// 3) PublishedAt — в UTC, допускается нулевое значение.
// 4) Реализация обязана уважать ctx (отмена/таймауты).
//
// ParseMany должен отправить по одному ParseResult на каждый URL и затем закрыть канал.
// Порядок результатов не гарантируется.
type Parser interface {
	ParseMany(ctx context.Context, urls []string) <-chan ParseResult
}

// ParseResult — результат парсинга одной ленты.
// Если Err != nil, Items может быть неполным или пустым.
type ParseResult struct {
	URL   string
	Items []models.News
	Err   error
}
