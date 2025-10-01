package models

type NewsListRequest struct {
	Limit     int32  `json:"limit"`      // == proto limit
	PageToken string `json:"page_token"` // == proto page_token
}

type NewsListResponse struct {
	Items         []News `json:"items"`
	NextPageToken string `json:"next_page_token"`
}

type NewsGetRequest struct {
	ID string `json:"id"`
}

type NewsGetResponse struct {
	Item *News `json:"item"`
}

type News struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Category         string `json:"category"`
	ShortDescription string `json:"short_description"`
	LongDescription  string `json:"long_description"`
	Link             string `json:"link"`
	ImageURL         string `json:"image_url"`
	PublishedAt      int64  `json:"published_at"` // Unix UTC
	FetchedAt        int64  `json:"fetched_at"`   // Unix UTC
}
