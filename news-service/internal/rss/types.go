// rss - реализует service.Parser для RSS 2.0.
package rss

// rss - корневая структура RSS-ленты.
type rss struct {
	Channel channel `xml:"channel"`
}

// channel - RSS-канал, содержащий список новостей.
type channel struct {
	Items []item `xml:"item"`
}

// item описывает одну новость в RSS-ленте.
type item struct {
	// Title — заголовок новости.
	Title string `xml:"title"`
	// Link — ссылка на материал. Может быть пустым/мусорным у некоторых издателей,
	// тогда рассматриваем guid (если он — полноценный URL) как fallback.
	Link string `xml:"link"`
	// GUID — «перманентный» идентификатор записи. У некоторых источников он
	// содержит URL и может использоваться как Link, даже если isPermaLink="false".
	GUID guid `xml:"guid"`
	// PubDate — дата публикации в строковом виде.
	PubDate string `xml:"pubDate"`
	// Description — краткое описание/тизер. Часто приходит внутри CDATA и с HTML.
	Description string `xml:"description"`
	// Categories — список категорий. В доменную модель кладём первую (как «основную»).
	Categories []string `xml:"category"`
	// ContentHTML — расширение content:encoded с полным HTML-телом.
	ContentHTML string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	// Enclosures — вложения (изображения, аудио и т.п.).
	//
	// Для картинок: берём image/*, если несколько — последний
	// или самый «тяжёлый» по length.
	Enclosures []enclosure `xml:"enclosure"`
	// MediaContent/MediaThumbs — второй приоритет для ImageURL.
	MediaContent []mediaEntry `xml:"http://search.yahoo.com/mrss/ content"`
	MediaThumbs  []mediaEntry `xml:"http://search.yahoo.com/mrss/ thumbnail"`
}

// guid — обёртка над <guid> с атрибутом isPermaLink.
type guid struct {
	// IsPermaLink — строковый флаг "true"/"false".
	IsPermaLink string `xml:"isPermaLink,attr"`
	// Value — текстовое содержимое <guid>.
	Value string `xml:",chardata"`
}

// enclosure — описание вложения RSS.
type enclosure struct {
	// URL — абсолютная ссылка на ресурс.
	URL string `xml:"url,attr"`
	// Type — медиатип.
	Type string `xml:"type,attr"`
	// Length — размер в байтах.
	Length int64 `xml:"length,attr"`
}

// mediaEntry — элемент Media RSS (media:content или media:thumbnail).
type mediaEntry struct {
	// URL — ссылка на ресурс.
	URL string `xml:"url,attr"`
	// Type — медиатип.
	Type string `xml:"type,attr"`
}
