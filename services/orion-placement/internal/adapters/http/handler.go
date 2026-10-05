package httpadapter

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/services/orion-placement/internal/application"
	"github.com/horizon/orion/services/orion-placement/internal/domain"
	"github.com/horizon/orion/services/orion-placement/internal/ports"
)

type Handler struct {
	logger  *slog.Logger
	service *application.Service
}

func NewHandler(logger *slog.Logger, service *application.Service) *Handler {
	return &Handler{
		logger:  logger,
		service: service,
	}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestID)

	r.Get("/healthz", h.handleHealth)
	r.Get("/readyz", h.handleReady)

	r.Route("/v1/hosts", func(r chi.Router) {
		r.Get("/", h.handleListHosts)
		r.Post("/", h.handleRegisterHost)
		r.Get("/{hostID}", h.handleGetHost)
		r.Post("/{hostID}/enable", h.handleEnableHost)
		r.Post("/{hostID}/disable", h.handleDisableHost)
		r.Post("/{hostID}/drain", h.handleDrainHost)
		r.Post("/{hostID}/undrain", h.handleUndrainHost)
	})

	r.Route("/v1/selections", func(r chi.Router) {
		r.Post("/hosts", h.handleSelectHost)
	})

	r.Route("/v1/allocations", func(r chi.Router) {
		r.Post("/release", h.handleReleaseHost)
	})

	r.Route("/v1/reservations", func(r chi.Router) {
		r.Get("/", h.handleListReservations)
		r.Post("/", h.handleCreateReservation)
		r.Get("/{reservationID}", h.handleGetReservation)
		r.Delete("/{reservationID}", h.handleDeleteReservation)
	})

	r.Route("/v1/resource-providers", func(r chi.Router) {
		r.Get("/", h.handleListResourceProviders)
		r.Post("/", h.handleCreateResourceProvider)
		r.Get("/{providerID}", h.handleGetResourceProvider)
		r.Patch("/{providerID}", h.handleUpdateResourceProvider)
		r.Delete("/{providerID}", h.handleDeleteResourceProvider)
	})

	return r
}

func (h *Handler) handleListResourceProviders(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListResourceProviders(r.Context(), r.URL.Query().Get("root_provider_id"))
	if err != nil {
		h.writePlacementError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]domain.ResourceProvider{"resource_providers": items})
}

func (h *Handler) handleCreateResourceProvider(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateResourceProviderRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateResourceProvider(r.Context(), req)
	if err != nil {
		h.writePlacementError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]domain.ResourceProvider{"resource_provider": item})
}
func (h *Handler) handleGetResourceProvider(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.GetResourceProvider(r.Context(), chi.URLParam(r, "providerID"))
	if err != nil {
		h.writePlacementError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]domain.ResourceProvider{"resource_provider": item})
}
func (h *Handler) handleUpdateResourceProvider(w http.ResponseWriter, r *http.Request) {
	var req domain.UpdateResourceProviderRequest
	req.UUID = chi.URLParam(r, "providerID")
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.UpdateResourceProvider(r.Context(), req)
	if err != nil {
		h.writePlacementError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]domain.ResourceProvider{"resource_provider": item})
}
func (h *Handler) handleDeleteResourceProvider(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteResourceProvider(r.Context(), chi.URLParam(r, "providerID")); err != nil {
		h.writePlacementError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writePlacementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrResourceProviderNotFound):
		apierror.Write(w, http.StatusNotFound, "resource_provider_not_found", "resource provider not found")
	case errors.Is(err, application.ErrResourceProviderConflict):
		apierror.Write(w, http.StatusConflict, "resource_provider_conflict", "resource provider hierarchy conflict")
	default:
		h.logger.Error("placement resource provider request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts := h.service.ListHosts(r.Context())
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"hosts": hosts})
}

func (h *Handler) handleRegisterHost(w http.ResponseWriter, r *http.Request) {
	var req domain.RegisterHostRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	host := h.service.RegisterHost(r.Context(), req)
	httpx.WriteJSON(w, http.StatusCreated, map[string]domain.Host{"host": host})
}

func (h *Handler) handleGetHost(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "hostID")
	host, err := h.service.GetHost(r.Context(), hostID)
	if err != nil {
		if errors.Is(err, application.ErrHostNotFound) {
			apierror.Write(w, http.StatusNotFound, "host_not_found", "host not found")
			return
		}
		h.logger.Error("placement get host failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]domain.Host{"host": host})
}

func (h *Handler) handleEnableHost(w http.ResponseWriter, r *http.Request) {
	enabled := true
	h.updateHostState(w, r, &enabled, nil)
}

func (h *Handler) handleDisableHost(w http.ResponseWriter, r *http.Request) {
	enabled := false
	h.updateHostState(w, r, &enabled, nil)
}

func (h *Handler) handleDrainHost(w http.ResponseWriter, r *http.Request) {
	drained := true
	h.updateHostState(w, r, nil, &drained)
}

func (h *Handler) handleUndrainHost(w http.ResponseWriter, r *http.Request) {
	drained := false
	h.updateHostState(w, r, nil, &drained)
}

func (h *Handler) updateHostState(w http.ResponseWriter, r *http.Request, enabled *bool, drained *bool) {
	hostID := chi.URLParam(r, "hostID")

	var req domain.UpdateHostStateRequest
	req.HostID = hostID
	req.Enabled = enabled
	req.Drained = drained

	host, err := h.service.UpdateHostState(r.Context(), req)
	if err != nil {
		if errors.Is(err, application.ErrHostNotFound) {
			apierror.Write(w, http.StatusNotFound, "host_not_found", "host not found")
			return
		}
		h.logger.Error("placement update host state failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]domain.Host{"host": host})
}

func (h *Handler) handleSelectHost(w http.ResponseWriter, r *http.Request) {
	var req domain.SelectHostRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	selection, err := h.service.SelectHost(r.Context(), req)
	if err != nil {
		if errors.Is(err, application.ErrNoValidHost) {
			apierror.Write(w, http.StatusConflict, "no_valid_host", "no valid host")
			return
		}
		h.logger.Error("placement selection failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]domain.HostSelection{"selection": selection})
}

func (h *Handler) handleReleaseHost(w http.ResponseWriter, r *http.Request) {
	var req domain.ReleaseHostRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if err := h.service.ReleaseHost(r.Context(), req); err != nil {
		if errors.Is(err, application.ErrHostNotFound) {
			apierror.Write(w, http.StatusNotFound, "host_not_found", "host not found")
			return
		}
		h.logger.Error("placement release failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func (h *Handler) handleListReservations(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		apierror.Write(w, http.StatusBadRequest, "missing_project_id", "project_id query parameter required")
		return
	}
	reservations, err := h.service.ListReservations(r.Context(), projectID)
	if err != nil {
		h.logger.Error("list reservations failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"reservations": reservations})
}

func (h *Handler) handleCreateReservation(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateReservationRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	reservation, err := h.service.CreateReservation(r.Context(), req)
	if err != nil {
		if errors.Is(err, ports.ErrHostRecordNotFound) {
			apierror.Write(w, http.StatusNotFound, "host_not_found", "host not found")
			return
		}
		if errors.Is(err, ports.ErrInsufficientCapacity) {
			apierror.Write(w, http.StatusConflict, "insufficient_capacity", "insufficient host capacity")
			return
		}
		h.logger.Error("create reservation failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]domain.Reservation{"reservation": reservation})
}

func (h *Handler) handleGetReservation(w http.ResponseWriter, r *http.Request) {
	reservationID := chi.URLParam(r, "reservationID")
	reservation, err := h.service.GetReservation(r.Context(), reservationID)
	if err != nil {
		if errors.Is(err, application.ErrReservationNotFound) {
			apierror.Write(w, http.StatusNotFound, "not_found", "reservation not found")
			return
		}
		h.logger.Error("get reservation failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]domain.Reservation{"reservation": reservation})
}

func (h *Handler) handleDeleteReservation(w http.ResponseWriter, r *http.Request) {
	reservationID := chi.URLParam(r, "reservationID")
	if err := h.service.DeleteReservation(r.Context(), reservationID); err != nil {
		h.logger.Error("delete reservation failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
