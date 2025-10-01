package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	usersv1 "github.com/pribylovaa/go-news-aggregator/api-gateway/gen/go/users"
	apierrors "github.com/pribylovaa/go-news-aggregator/api-gateway/internal/errors"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/models"
)

func (h *Handlers) GetProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.Users.ProfileByID(r.Context(), &usersv1.ProfileByIDRequest{UserId: id})
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.UserFromProto(resp))
}

func (h *Handlers) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	var in models.UpdateUserRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	in.UserID = id // user_id берём из пути.
	resp, err := h.Clients.Users.UpdateProfile(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.UserFromProto(resp))
}

func (h *Handlers) AvatarPresign(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	var in models.AvatarPresignRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	in.UserID = id

	resp, err := h.Clients.Users.AvatarUploadURL(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.AvatarPresignFromProto(resp))
}

func (h *Handlers) AvatarConfirm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	var in models.AvatarConfirmRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	in.UserID = id

	resp, err := h.Clients.Users.ConfirmAvatarUpload(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.UserFromProto(resp))
}
