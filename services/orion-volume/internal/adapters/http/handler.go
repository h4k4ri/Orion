package httpadapter

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/httpx"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-volume/internal/application"
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

	r.Route("/v1/volumes", func(r chi.Router) {
		r.Get("/", h.handleListVolumes)
		r.Post("/", h.handleCreateVolume)
		r.Get("/{volumeID}", h.handleGetVolume)
		r.Delete("/{volumeID}", h.handleDeleteVolume)
		r.Post("/{volumeID}/attach", h.handleAttach)
		r.Post("/{volumeID}/detach", h.handleDetach)
	})
	r.Route("/v1/snapshots", func(r chi.Router) {
		r.Get("/", h.handleListSnapshots)
		r.Post("/", h.handleCreateSnapshot)
		r.Get("/{snapshotID}", h.handleGetSnapshot)
		r.Delete("/{snapshotID}", h.handleDeleteSnapshot)
		r.Post("/{snapshotID}/restore", h.handleRestoreSnapshot)
	})

	return r
}

func (h *Handler) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListSnapshots(r.Context(), r.URL.Query().Get("project_id"), r.URL.Query().Get("volume_id"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]volumekit.VolumeSnapshot{"snapshots": items})
}
func (h *Handler) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req volumekit.CreateVolumeSnapshotRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateSnapshot(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]volumekit.VolumeSnapshot{"snapshot": item})
}
func (h *Handler) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.GetSnapshot(r.Context(), chi.URLParam(r, "snapshotID"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]volumekit.VolumeSnapshot{"snapshot": item})
}
func (h *Handler) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteSnapshot(r.Context(), chi.URLParam(r, "snapshotID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) handleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.RestoreSnapshot(r.Context(), chi.URLParam(r, "snapshotID"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]volumekit.Volume{"volume": item})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handleListVolumes(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string][]volumekit.Volume{"volumes": h.service.ListVolumes(r.Context())})
}

func (h *Handler) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	var req volumekit.CreateVolumeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateVolume(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]volumekit.Volume{"volume": item})
}

func (h *Handler) handleGetVolume(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "volumeID")
	item, err := h.service.GetVolume(r.Context(), volumeID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]volumekit.Volume{"volume": item})
}

func (h *Handler) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "volumeID")
	if err := h.service.DeleteVolume(r.Context(), volumeID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleAttach(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "volumeID")
	var req volumekit.AttachVolumeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.AttachVolume(r.Context(), volumeID, req.ServerID, req.HostID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]volumekit.Volume{"volume": item})
}

func (h *Handler) handleDetach(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "volumeID")
	item, err := h.service.DetachVolume(r.Context(), volumeID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]volumekit.Volume{"volume": item})
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrBackendUnavailable):
		apierror.Write(w, http.StatusServiceUnavailable, "backend_unavailable", "volume backend unavailable")
	case errors.Is(err, application.ErrVolumeNotFound):
		apierror.Write(w, http.StatusNotFound, "volume_not_found", "volume not found")
	case errors.Is(err, application.ErrInvalidSize):
		apierror.Write(w, http.StatusBadRequest, "invalid_size", "invalid volume size")
	case errors.Is(err, application.ErrVolumeInUse):
		apierror.Write(w, http.StatusConflict, "volume_in_use", "volume in use")
	case errors.Is(err, application.ErrVolumeNotAttached):
		apierror.Write(w, http.StatusConflict, "volume_not_attached", "volume not attached")
	case errors.Is(err, application.ErrNoEligibleHost):
		apierror.Write(w, http.StatusConflict, "no_eligible_host", "no eligible volume host")
	case errors.Is(err, application.ErrHostMismatch):
		apierror.Write(w, http.StatusConflict, "host_mismatch", "volume is bound to a different host")
	case errors.Is(err, application.ErrQuotaExceeded):
		apierror.Write(w, http.StatusTooManyRequests, "quota_exceeded", "project quota exceeded")
	case errors.Is(err, application.ErrSnapshotNotFound):
		apierror.Write(w, http.StatusNotFound, "snapshot_not_found", "volume snapshot not found")
	case errors.Is(err, application.ErrSnapshotUnsupported):
		apierror.Write(w, http.StatusNotImplemented, "snapshot_unsupported", "volume snapshots are not supported")
	case errors.Is(err, application.ErrSnapshotInUse):
		apierror.Write(w, http.StatusConflict, "snapshot_in_use", "volume cannot be snapshotted in its current state")
	default:
		h.logger.Error("volume request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
