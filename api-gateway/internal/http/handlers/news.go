package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	apierrors "github.com/pribylovaa/go-news-aggregator/api-gateway/internal/errors"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/models"
)

func (h *Handlers) ListNews(w http.ResponseWriter, r *http.Request) {
	var req models.NewsListRequest
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			apierrors.WriteError(w, r, statusErrorInvalidArgument())
			return
		}

		req.Limit = int32(n)
	}

	req.PageToken = r.URL.Query().Get("page_token")

	resp, err := h.Clients.News.ListNews(r.Context(), req.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.NewsListFromProto(resp))
}

func (h *Handlers) GetNewsByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.News.NewsByID(r.Context(), (&models.NewsGetRequest{ID: id}).ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.NewsGetFromProto(resp))
}
