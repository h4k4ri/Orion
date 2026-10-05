package httpadapter

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/image"
	"github.com/horizon/orion/services/orion-image/internal/application"
)

type Handler struct {
	logger  *slog.Logger
	service *application.Service
}

func NewHandler(logger *slog.Logger, service *application.Service) *Handler {
	return &Handler{logger: logger, service: service}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestID)

	r.Get("/healthz", h.handleHealth)
	r.Get("/readyz", h.handleReady)

	r.Route("/v1/images", func(r chi.Router) {
		r.Get("/", h.handleListImages)
		r.Post("/", h.handleCreateImage)
		r.Get("/{imageID}", h.handleGetImage)
		r.Delete("/{imageID}", h.handleDeleteImage)
		r.Get("/{imageID}/file", h.handleGetImageFile)
		r.Put("/{imageID}/file", h.handleUploadImage)
	})

	return r
}

func (h *Handler) handleCreateImage(w http.ResponseWriter, r *http.Request) {
	var metadata image.Image
	if err := httpx.DecodeJSON(r, &metadata); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateImage(r.Context(), metadata)
	if err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_image", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]image.Image{"image": item})
}

func (h *Handler) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.UploadImage(r.Context(), chi.URLParam(r, "imageID"), r.Body)
	if err != nil {
		h.writeImageError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]image.Image{"image": item})
}

func (h *Handler) handleDeleteImage(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteImage(r.Context(), chi.URLParam(r, "imageID")); err != nil {
		h.writeImageError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handleListImages(w http.ResponseWriter, r *http.Request) {
	images := h.service.ListImages(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string][]image.Image{"images": images})
}

func (h *Handler) handleGetImage(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "imageID")
	img, err := h.service.GetImage(r.Context(), imageID)
	if err != nil {
		if errors.Is(err, application.ErrImageNotFound) {
			apierror.Write(w, http.StatusNotFound, "image_not_found", "image not found")
			return
		}
		h.logger.Error("image request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]image.Image{"image": img})
}

func (h *Handler) handleGetImageFile(w http.ResponseWriter, r *http.Request) {
	imageID := chi.URLParam(r, "imageID")
	imageID = strings.TrimSuffix(imageID, "/file")

	img, err := h.service.GetImage(r.Context(), imageID)
	if err != nil {
		if errors.Is(err, application.ErrImageNotFound) {
			apierror.Write(w, http.StatusNotFound, "image_not_found", "image not found")
			return
		}
		h.logger.Error("image request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+imageID+"\"")
	http.ServeFile(w, r, img.Path)
}

func (h *Handler) writeImageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrImageNotFound):
		apierror.Write(w, http.StatusNotFound, "image_not_found", "image not found")
	case errors.Is(err, application.ErrImageProtected):
		apierror.Write(w, http.StatusConflict, "image_protected", "image is protected")
	case errors.Is(err, application.ErrImageTooLarge):
		apierror.Write(w, http.StatusRequestEntityTooLarge, "image_too_large", "image is too large")
	default:
		h.logger.Error("image request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
