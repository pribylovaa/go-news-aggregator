package service

import (
	"reflect"
	"testing"
	"time"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/models"
)

func Test_finalizeNews(t *testing.T) {
	t.Parallel()

	utc := time.UTC

	tests := []struct {
		name   string
		news   models.News
		nowUTC time.Time
		want   models.News
		want2  bool
	}{
		{
			name:   "reject: empty title",
			news:   models.News{Title: "", Link: "https://a"},
			nowUTC: time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
			want:   models.News{},
			want2:  false,
		},
		{
			name:   "reject: title is only spaces",
			news:   models.News{Title: " \t\n ", Link: "https://a"},
			nowUTC: time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
			want:   models.News{},
			want2:  false,
		},
		{
			name:   "reject: empty link",
			news:   models.News{Title: "ok", Link: ""},
			nowUTC: time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
			want:   models.News{},
			want2:  false,
		},
		{
			name:   "reject: link is only spaces",
			news:   models.News{Title: "ok", Link: "   \t"},
			nowUTC: time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
			want:   models.News{},
			want2:  false,
		},
		{
			name: "ok: trims title/link/short; preserves category/image; long stays as is (non-empty); converts PublishedAt to UTC; FetchedAt=now",
			news: models.News{
				Title:            "  Hello  ",
				Category:         " world ",
				ShortDescription: "  teaser ",
				LongDescription:  "  long body  ",
				Link:             "   https://example/a ",
				ImageURL:         "  https://img/c.jpg  ",
				PublishedAt:      time.Date(2024, 7, 1, 15, 30, 0, 0, time.FixedZone("MSK", 3*3600)),
				FetchedAt:        time.Date(2000, 1, 1, 0, 0, 0, 0, utc),
			},
			nowUTC: time.Date(2024, 7, 1, 10, 30, 0, 0, utc),
			want: models.News{
				Title:            "Hello",
				Category:         " world ",
				ShortDescription: "teaser",
				LongDescription:  "  long body  ",
				Link:             "https://example/a",
				ImageURL:         "  https://img/c.jpg  ",
				PublishedAt:      time.Date(2024, 7, 1, 12, 30, 0, 0, utc),
				FetchedAt:        time.Date(2024, 7, 1, 10, 30, 0, 0, utc),
			},
			want2: true,
		},
		{
			name: "ok: long description falls back to short when long is empty/whitespace",
			news: models.News{
				Title:            "T",
				Link:             "https://example.org",
				ShortDescription: " short ",
				LongDescription:  "   \t",
			},
			nowUTC: time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
			want: models.News{
				Title:            "T",
				Link:             "https://example.org",
				ShortDescription: "short",
				LongDescription:  "short",
				PublishedAt:      time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
				FetchedAt:        time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
			},
			want2: true,
		},
		{
			name: "ok: both long and short empty -> both remain empty",
			news: models.News{
				Title:            "T",
				Link:             "https://example.org",
				ShortDescription: "",
				LongDescription:  "   ",
			},
			nowUTC: time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
			want: models.News{
				Title:       "T",
				Link:        "https://example.org",
				PublishedAt: time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
				FetchedAt:   time.Date(2024, 7, 1, 12, 0, 0, 0, utc),
			},
			want2: true,
		},
		{
			name: "ok: zero PublishedAt -> replaced with nowUTC",
			news: models.News{
				Title:       "A",
				Link:        "https://a",
				PublishedAt: time.Time{},
			},
			nowUTC: time.Date(2025, 1, 2, 3, 4, 5, 0, utc),
			want: models.News{
				Title:       "A",
				Link:        "https://a",
				PublishedAt: time.Date(2025, 1, 2, 3, 4, 5, 0, utc),
				FetchedAt:   time.Date(2025, 1, 2, 3, 4, 5, 0, utc),
			},
			want2: true,
		},
		{
			name: "ok: non-zero PublishedAt -> converted to UTC (keeps instant), FetchedAt always nowUTC",
			news: models.News{
				Title:       "A",
				Link:        "https://a",
				PublishedAt: time.Date(2024, 12, 31, 23, 59, 0, 0, time.FixedZone("MSK", 3*3600)),
			},
			nowUTC: time.Date(2025, 1, 2, 3, 4, 5, 0, utc),
			want: models.News{
				Title:       "A",
				Link:        "https://a",
				PublishedAt: time.Date(2024, 12, 31, 20, 59, 0, 0, utc),
				FetchedAt:   time.Date(2025, 1, 2, 3, 4, 5, 0, utc),
			},
			want2: true,
		},
		{
			name: "ok: FetchedAt input is ignored and overridden with nowUTC",
			news: models.News{
				Title:     "X",
				Link:      "https://x",
				FetchedAt: time.Date(2000, 1, 1, 0, 0, 0, 0, utc),
			},
			nowUTC: time.Date(2026, 2, 3, 4, 5, 6, 0, utc),
			want: models.News{
				Title:       "X",
				Link:        "https://x",
				PublishedAt: time.Date(2026, 2, 3, 4, 5, 6, 0, utc),
				FetchedAt:   time.Date(2026, 2, 3, 4, 5, 6, 0, utc),
			},
			want2: true,
		},
		{
			name: "ok: allows empty optional fields (category/image/desc) when title/link valid",
			news: models.News{
				Title: "Ok",
				Link:  "https://ok",
			},
			nowUTC: time.Date(2024, 3, 4, 5, 6, 7, 0, utc),
			want: models.News{
				Title:       "Ok",
				Link:        "https://ok",
				PublishedAt: time.Date(2024, 3, 4, 5, 6, 7, 0, utc),
				FetchedAt:   time.Date(2024, 3, 4, 5, 6, 7, 0, utc),
			},
			want2: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, got2 := finalizeNews(tt.news, tt.nowUTC)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("finalizeNews() got = %#v,\nwant = %#v", got, tt.want)
			}
			if got2 != tt.want2 {
				t.Errorf("finalizeNews() ok = %v, want %v", got2, tt.want2)
			}
		})
	}
}
