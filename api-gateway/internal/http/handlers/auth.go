package handlers

import (
	"net/http"

	apierrors "github.com/pribylovaa/go-news-aggregator/api-gateway/internal/errors"
	"github.com/pribylovaa/go-news-aggregator/api-gateway/internal/models"
)

func (h *Handlers) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var in models.AuthRegisterRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.Auth.RegisterUser(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	out := models.AuthFromProto(resp)
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) LoginUser(w http.ResponseWriter, r *http.Request) {
	var in models.AuthLoginRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.Auth.LoginUser(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.AuthFromProto(resp))
}

func (h *Handlers) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var in models.AuthRefreshRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.Auth.RefreshToken(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.AuthFromProto(resp))
}

func (h *Handlers) RevokeToken(w http.ResponseWriter, r *http.Request) {
	var in models.AuthRevokeRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.Auth.RevokeToken(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.AuthRevokeFromProto(resp))
}

func (h *Handlers) ValidateToken(w http.ResponseWriter, r *http.Request) {
	var in models.AuthValidateRequest
	if err := decodeStrict(r, &in); err != nil {
		apierrors.WriteError(w, r, statusErrorInvalidArgument())
		return
	}

	resp, err := h.Clients.Auth.ValidateToken(r.Context(), in.ToProto())
	if err != nil {
		apierrors.WriteError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models.AuthValidateFromProto(resp))
}
