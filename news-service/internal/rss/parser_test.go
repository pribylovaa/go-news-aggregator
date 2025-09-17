package rss

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pribylovaa/go-news-aggregator/news-service/internal/service"
	"github.com/stretchr/testify/require"
)

// mkRSS — собирает минимальный RSS 2.0 документ с нужными namespace.
func mkRSS(items ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"
     xmlns:content="http://purl.org/rss/1.0/modules/content/"
     xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    ` + strings.Join(items, "\n") + `
  </channel>
</rss>`
}

// mkItem — утилита шаблона <item>.
func mkItem(fields map[string]string) string {
	var b strings.Builder
	b.WriteString("<item>\n")

	for tag, val := range fields {
		switch tag {
		case "title", "link", "pubDate", "description", "category":
			b.WriteString(fmt.Sprintf("<%s>%s</%s>\n", tag, val, tag))
		case "guid":
			isPerm := ""
			value := val
			if left, right, ok := strings.Cut(val, "|"); ok {
				isPerm, value = left, right
			}

			if isPerm == "" {
				b.WriteString(fmt.Sprintf("<guid>%s</guid>\n", value))
			} else {
				b.WriteString(fmt.Sprintf("<guid isPermaLink=\"%s\">%s</guid>\n", isPerm, value))
			}
		}
	}

	// content:encoded (HTML)
	if v, ok := fields["content"]; ok {
		b.WriteString(fmt.Sprintf("<content:encoded><![CDATA[%s]]></content:encoded>\n", v))
	}

	// enclosure (можно несколько через ; как url|type|length)
	if v, ok := fields["enclosures"]; ok && v != "" {
		parts := strings.Split(v, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			u, typ, ln := p, "", ""
			if i := strings.Index(u, "|"); i >= 0 {
				typ = u[i+1:]
				u = u[:i]
			}
			if j := strings.Index(typ, "|"); j >= 0 {
				ln = typ[j+1:]
				typ = typ[:j]
			}
			if typ == "" {
				typ = "image/jpeg"
			}
			if ln == "" {
				b.WriteString(fmt.Sprintf(`<enclosure url="%s" type="%s"/>`+"\n", u, typ))
			} else {
				b.WriteString(fmt.Sprintf(`<enclosure url="%s" type="%s" length="%s"/>`+"\n", u, typ, ln))
			}
		}
	}

	// media:content / media:thumbnail
	if v, ok := fields["mediaContent"]; ok && v != "" {
		parts := strings.Split(v, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			u, typ := p, ""
			if i := strings.Index(u, "|"); i >= 0 {
				typ = u[i+1:]
				u = u[:i]
			}
			if typ == "" {
				b.WriteString(fmt.Sprintf(`<media:content url="%s"/>`+"\n", u))
			} else {
				b.WriteString(fmt.Sprintf(`<media:content url="%s" type="%s"/>`+"\n", u, typ))
			}
		}
	}
	if v, ok := fields["mediaThumb"]; ok && v != "" {
		parts := strings.Split(v, ";")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			b.WriteString(fmt.Sprintf(`<media:thumbnail url="%s"/>`+"\n", p))
		}
	}
	b.WriteString("</item>")
	return b.String()
}

// Test_parsePubDate — проверяем набор популярных форматов и ошибку на пустое значение.
func Test_parsePubDate(t *testing.T) {
	t.Parallel()

	type tc struct {
		in   string
		want time.Time
		ok   bool
	}
	cases := []tc{
		{"Tue, 16 Sep 2025 12:34:56 +0300", time.Date(2025, 9, 16, 9, 34, 56, 0, time.UTC), true},
		{"Tue, 16 Sep 2025 12:34:56 GMT", time.Date(2025, 9, 16, 12, 34, 56, 0, time.UTC), true},
		{"Tue, 16 Sep 25 12:34:56 +0300", time.Date(2025, 9, 16, 9, 34, 56, 0, time.UTC), true},
		{"2025-09-16T12:34:56+03:00", time.Date(2025, 9, 16, 9, 34, 56, 0, time.UTC), true},
		{"", time.Time{}, false},
	}
	for _, c := range cases {
		got, err := parsePubDate(c.in)
		if c.ok {
			require.NoError(t, err, c.in)
			require.True(t, got.Equal(c.want), "in=%q got=%s want=%s", c.in, got, c.want)
		} else {
			require.Error(t, err)
		}
	}
}

// Test_firstImgSrc — извлечение <img src="..."> из HTML.
func Test_firstImgSrc(t *testing.T) {
	t.Parallel()
	html := `<div><p>text</p><img src="https://cdn.example.org/a.jpg" alt="x"></div>`
	require.Equal(t, "https://cdn.example.org/a.jpg", firstImgSrc(html))
	require.Equal(t, "", firstImgSrc("<p>no image</p>"))
}

// Test_pickImageURL_Priorities — enclosure > media:content > media:thumb > <img>.
func Test_pickImageURL_Priorities(t *testing.T) {
	t.Parallel()

	// 1) enclosure с разными length: берём самый большой; если length нет — последний.
	it1 := item{
		Enclosures: []enclosure{
			{URL: "https://cdn.example.org/e1.jpg", Type: "image/jpeg", Length: 100},
			{URL: "https://cdn.example.org/e2.jpg", Type: "image/jpeg", Length: 200},
		},
	}
	require.Equal(t, "https://cdn.example.org/e2.jpg", pickImageURL(it1))

	it2 := item{
		Enclosures: []enclosure{
			{URL: "https://cdn.example.org/e1.jpg", Type: "image/jpeg"},
			{URL: "https://cdn.example.org/e2.jpg", Type: "image/jpeg"},
		},
	}
	require.Equal(t, "https://cdn.example.org/e2.jpg", pickImageURL(it2))

	// 2) media:content, если enclosure нет.
	it3 := item{
		MediaContent: []mediaEntry{
			{URL: "https://cdn.example.org/m1.jpg", Type: "image/jpeg"},
		},
	}
	require.Equal(t, "https://cdn.example.org/m1.jpg", pickImageURL(it3))

	// 3) media:thumb, если media:content нет.
	it4 := item{MediaThumbs: []mediaEntry{{URL: "https://cdn.example.org/t.jpg"}}}
	require.Equal(t, "https://cdn.example.org/t.jpg", pickImageURL(it4))

	// 4) <img> в content:encoded, затем в description.
	it5 := item{
		ContentHTML: `<p>hello<img src="https://cdn.example.org/c.jpg"></p>`,
		Description: `<p><img src="https://cdn.example.org/d.jpg"></p>`,
	}
	require.Equal(t, "https://cdn.example.org/c.jpg", pickImageURL(it5))

	it6 := item{
		ContentHTML: `<p>no img</p>`,
		Description: `<p><img src="https://cdn.example.org/d.jpg"></p>`,
	}
	require.Equal(t, "https://cdn.example.org/d.jpg", pickImageURL(it6))
}

// Test_canonicalLink — нормализация ссылок и fallback на GUID.
func Test_canonicalLink(t *testing.T) {
	t.Parallel()

	// Нормализация.
	u := canonicalLink("https://example.org/a?utm_source=x&utm_medium=y#frag", guid{})
	require.Equal(t, "https://example.org/a", u)

	// Fallback на GUID-URL при пустом link.
	u2 := canonicalLink("", guid{IsPermaLink: "false", Value: "https://example.org/gid?a=1#z"})
	require.Equal(t, "https://example.org/gid?a=1", u2)

	// Если строка не парсится как URL — возвращаем как есть.
	raw := "not a url value"
	require.Equal(t, raw, canonicalLink(raw, guid{}))
}

// Test_ParseMany_HappyPath_And_Errors — один URL успешный, второй — 500.
func Test_ParseMany_HappyPath_And_Errors(t *testing.T) {
	t.Parallel()

	// feed OK: два item
	okFeed := mkRSS(
		mkItem(map[string]string{
			"title":       "  Hello  ",
			"link":        "https://example.org/a?utm_source=rss#frag",
			"pubDate":     "Tue, 16 Sep 2025 12:00:00 +0300",
			"description": "  teaser ",
			"content":     `<p>body<img src="https://cdn.example.org/a.jpg"></p>`,
			"category":    "World",
			"enclosures":  "https://cdn.example.org/e.jpg|image/jpeg|123",
		}),
		mkItem(map[string]string{
			"title":        "No Link but GUID",
			"link":         "",
			"guid":         "false|https://example.org/guid",
			"pubDate":      "Tue, 16 Sep 2025 12:00:00 GMT",
			"description":  "d",
			"mediaContent": "https://cdn.example.org/m.jpg|image/jpeg",
		}),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(okFeed))
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := srv.Client()
	p := New(client, 4)

	ctx := context.Background()
	results := p.ParseMany(ctx, []string{srv.URL + "/ok", srv.URL + "/fail"})

	got := map[string]service.ParseResult{}
	for r := range results {
		got[r.URL] = r
	}

	require.Len(t, got, 2)
	// Ошибочный URL.
	require.Error(t, got[srv.URL+"/fail"].Err)

	// Успешный URL.
	ok := got[srv.URL+"/ok"]
	require.NoError(t, ok.Err)
	require.Len(t, ok.Items, 2)

	// Сортировка по link.
	sort.Slice(ok.Items, func(i, j int) bool { return ok.Items[i].Link < ok.Items[j].Link })

	it1 := ok.Items[0]
	require.Equal(t, "Hello", it1.Title)
	require.Equal(t, "World", it1.Category)
	require.Equal(t, "teaser", it1.ShortDescription)
	require.Equal(t, `<p>body<img src="https://cdn.example.org/a.jpg"></p>`, it1.LongDescription)
	require.Equal(t, "https://example.org/a", it1.Link)
	require.Equal(t, "https://cdn.example.org/e.jpg", it1.ImageURL)
	require.True(t, it1.FetchedAt.IsZero(), "parser должен вернуть нулевой FetchedAt")
	require.Equal(t, time.Date(2025, 9, 16, 9, 0, 0, 0, time.UTC), it1.PublishedAt)

	it2 := ok.Items[1]
	require.Equal(t, "No Link but GUID", it2.Title)
	require.Equal(t, "https://example.org/guid", it2.Link)
	require.Equal(t, "https://cdn.example.org/m.jpg", it2.ImageURL)
	require.True(t, it2.FetchedAt.IsZero())
}

// Test_ParseMany_ContextCancel — «подвисающий» хендлер + короткий таймаут контекста.
func Test_ParseMany_ContextCancel(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(mkRSS()))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := srv.Client()
	p := New(client, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	urls := []string{srv.URL + "/slow"}
	got := make([]service.ParseResult, 0, len(urls))
	for r := range p.ParseMany(ctx, urls) {
		got = append(got, r)
	}

	require.Len(t, got, 1)
	require.Error(t, got[0].Err)
}
