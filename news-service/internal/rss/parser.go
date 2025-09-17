package rss

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pribylovaa/go-news-aggregator/auth-service/pkg/log"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
	"github.com/pribylovaa/go-news-aggregator/news-service/internal/service"
)

// Parser реализует service.Parser для RSS 2.0.
// Возвращает доменные объекты models.News с незаполненным FetchedAt.
//
// Параллелизм ограничен семафором maxConc. HTTP-клиент настраивается извне
// (таймауты, прокси и т.д.).
type Parser struct {
	client  *http.Client
	maxConc int
}

// New создаёт новый RSS-парсер.
func New(client *http.Client, maxConcurrent int) *Parser {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	if maxConcurrent <= 0 {
		maxConcurrent = 6
	}

	return &Parser{client: client, maxConc: maxConcurrent}
}

// ParseMany парсит несколько RSS-лент конкурентно и отдаёт результаты в канал.
// Канал закрывается после обработки всех URL.
func (p *Parser) ParseMany(ctx context.Context, urls []string) <-chan service.ParseResult {
	output := make(chan service.ParseResult)

	go func() {
		defer close(output)

		sem := make(chan struct{}, p.maxConc)

		for _, u := range urls {
			select {
			case <-ctx.Done():
				return
			default:
			}

			url := u
			sem <- struct{}{}

			go func() {
				defer func() {
					<-sem
				}()

				items, err := p.fetchOne(ctx, url)

				output <- service.ParseResult{URL: url, Items: items, Err: err}
			}()
		}

		for i := 0; i < cap(sem); i++ {
			sem <- struct{}{}
		}
	}()

	return output
}

// fetchOne загружает и парсит RSS по URL.
func (p *Parser) fetchOne(ctx context.Context, src string) ([]models.News, error) {
	const op = "rss.fetchOne"

	lg := log.From(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: new_request: %w", op, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		lg.Warn("http_error",
			slog.String("op", op),
			slog.String("url", src),
			slog.String("err", err.Error()),
		)
		return nil, fmt.Errorf("%s: do: %w", op, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("%s: status=%d", op, resp.StatusCode)
	}

	dec := xml.NewDecoder(resp.Body)
	var doc rss
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", op, err)
	}

	var output []models.News
	now := time.Time{}
	for _, item := range doc.Channel.Items {
		title := strings.TrimSpace(item.Title)
		link := canonicalLink(item.Link, item.GUID)

		if title == "" || link == "" {
			continue
		}

		pub, err := parsePubDate(item.PubDate)
		if err != nil {
			log.From(ctx).Warn("date_parse_failed",
				slog.String("op", op),
				slog.String("url", src),
				slog.String("value", item.PubDate),
				slog.String("err", err.Error()),
			)
		}

		output = append(output, models.News{
			Title:            title,
			Category:         firstOrEmptyCategory(item.Categories),
			ShortDescription: strings.TrimSpace(item.Description),
			LongDescription:  strings.TrimSpace(item.ContentHTML),
			Link:             link,
			ImageURL:         pickImageURL(item),
			PublishedAt:      pub,
			FetchedAt:        now,
		})
	}

	return output, nil
}

func firstOrEmptyCategory(categories []string) string {
	if len(categories) == 0 {
		return ""
	}

	return strings.TrimSpace(categories[0])
}

// pickImageURL выбирает URL обложки в порядке приоритетов:
// 1) enclosure image/* (если несколько — c max length, иначе последний);
// 2) media:content / media:thumbnail (image/* или пустой type);
// 3) первая <img src> из content:encoded, затем из description.
func pickImageURL(item item) string {
	var bestURL string
	var bestLen int64

	// 1) enclosure.
	for _, e := range item.Enclosures {
		if e.URL == "" {
			continue
		}

		if t := strings.ToLower(e.Type); t != "" && !strings.HasPrefix(t, "image/") {
			continue
		}

		if e.Length > 0 && e.Length >= bestLen {
			bestLen, bestURL = e.Length, e.URL
			continue
		}

		if bestLen == 0 {
			bestURL = e.URL
		}
	}

	if bestURL != "" {
		return bestURL
	}

	// 2) media:content / thumbnail.
	for _, m := range item.MediaContent {
		if m.URL == "" {
			continue
		}

		if m.Type == "" || strings.HasPrefix(strings.ToLower(m.Type), "image/") {
			return m.URL
		}
	}

	for _, m := range item.MediaThumbs {
		if m.URL != "" {
			return m.URL
		}
	}

	// 3) <img src>
	if u := firstImgSrc(item.ContentHTML); u != "" {
		return u
	}

	if u := firstImgSrc(item.Description); u != "" {
		return u
	}

	return ""
}

var reImg = regexp.MustCompile(`(?is)<img[^>]+src=["']([^"']+)["']`)

func firstImgSrc(html string) string {
	m := reImg.FindStringSubmatch(html)

	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}

	return ""
}

// canonicalLink нормализует ссылку: убирает фрагмент и трекинг.
func canonicalLink(raw string, g guid) string {
	str := strings.TrimSpace(raw)

	if str == "" {
		if url := strings.TrimSpace(g.Value); strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			str = url
		}
	}

	u, err := url.Parse(str)
	if err != nil {
		return str
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return str
	}

	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || strings.HasSuffix(lk, "clid") || strings.HasPrefix(lk, "mc_") || lk == "igshid" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()

	return u.String()
}

// parsePubDate пробует набор популярных форматов и возвращает UTC-время.
func parsePubDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty date")
	}

	layouts := []string{
		time.RFC1123Z,                   // Mon, 02 Jan 2006 15:04:05 -0700
		time.RFC1123,                    // Mon, 02 Jan 2006 15:04:05 MST
		"Mon, 02 Jan 06 15:04:05 -0700", // Mon, 02 Jan 06 15:04:05 -0700  (2-digit year)
		"Mon, 02 Jan 06 15:04:05 MST",   // Mon, 02 Jan 06 15:04:05 MST    (2-digit year)
		time.RFC822Z,                    // 02 Jan 06 15:04 -0700
		time.RFC822,                     // 02 Jan 06 15:04 MST
		time.RFC3339,                    // 2006-01-02T15:04:05Z07:00
		"Mon, 02 Jan 2006 15:04:05 MST", // нестандарт: с аббревиатурой без смещения
	}

	var lastErr error
	for _, l := range layouts {
		if t, err := time.Parse(l, value); err == nil {
			return t.UTC(), nil
		} else {
			lastErr = err
		}
	}

	return time.Time{}, lastErr
}
