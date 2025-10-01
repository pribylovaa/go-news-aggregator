package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	apierrors "github.com/pribylovaa/go-news-aggregator/api-gateway/internal/errors"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/models"
)

func (h *Handlers) CreateComment(w http.ResponseWriter, r *http.Request) {
	var in models.CreateCommentRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.Comments.CreateComment(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	out := models.CreateCommentFromProto(resp)
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handlers) GetCommentByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.Comments.CommentByID(r.Context(), (&models.GetCommentRequest{ID: id}).ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.GetCommentFromProto(resp))
}

func (h *Handlers) ListRootComments(w http.ResponseWriter, r *http.Request) {
	var req models.ListRootCommentsRequest
	req.NewsID = chi.URLParam(r, "news_id")
	if req.NewsID == "" {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	if v := r.URL.Query().Get("page_size"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			apierrors.WriteError(w, r, statusErrorInvalidArgument())
			return
		}

		req.PageSize = int32(n)
	}

	req.PageToken = r.URL.Query().Get("page_token")

	resp, err := h.Clients.Comments.ListByNews(r.Context(), req.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.ListRootCommentsFromProto(resp))
}

func (h *Handlers) ListReplies(w http.ResponseWriter, r *http.Request) {
	var req models.ListRepliesRequest
	req.ParentID = chi.URLParam(r, "id")
	if req.ParentID == "" {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	if v := r.URL.Query().Get("page_size"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			apierrors.WriteError(w, r, statusErrorInvalidArgument())
			return
		}

		req.PageSize = int32(n)
	}

	req.PageToken = r.URL.Query().Get("page_token")

	resp, err := h.Clients.Comments.ListReplies(r.Context(), req.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.ListRepliesFromProto(resp))
}
