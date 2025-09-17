package service

import (
	"strings"
	"time"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
)

// finalizeNews доводит запись до инвариантов домена:
//   - Title/Link обязательны (после TrimSpace) — иначе запись отбрасывается;
//   - LongDescription := LongDescription || ShortDescription;
//   - PublishedAt := PublishedAt || nowUTC (UTC);
//   - FetchedAt := nowUTC (перекрывает любые внешние значения).
//
// Возвращает (новость, ok=false если запись следует отбросить).
func finalizeNews(news models.News, nowUTC time.Time) (models.News, bool) {
	news.Title = strings.TrimSpace(news.Title)
	news.Link = strings.TrimSpace(news.Link)

	if news.Title == "" || news.Link == "" {
		return models.News{}, false
	}

	news.ShortDescription = strings.TrimSpace(news.ShortDescription)

	if strings.TrimSpace(news.LongDescription) == "" {
		news.LongDescription = news.ShortDescription
	}

	if news.PublishedAt.IsZero() {
		news.PublishedAt = nowUTC
	} else {
		news.PublishedAt = news.PublishedAt.UTC()
	}

	news.FetchedAt = nowUTC

	return news, true
}
