package httpadapter

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/task"
	"github.com/horizon/orion/services/orion-compute/internal/application"
	"github.com/horizon/orion/services/orion-compute/internal/domain"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
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

	r.Route("/v1/servers", func(r chi.Router) {
		r.Get("/", h.handleListServers)
		r.Post("/", h.handleCreateServer)
		r.Get("/{serverID}", h.handleGetServer)
		r.Delete("/{serverID}", h.handleDeleteServer)
		r.Post("/{serverID}/attach-volume", h.handleAttachVolume)
		r.Post("/{serverID}/detach-volume", h.handleDetachVolume)
	})

	r.Route("/v1/tasks", func(r chi.Router) {
		r.Get("/{taskID}", h.handleGetTask)
	})

	return r
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handleListServers(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string][]compute.Server{"servers": h.service.ListServers(r.Context())})
}

func (h *Handler) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var cmd domain.CreateServerCommand
	if err := httpx.DecodeJSON(r, &cmd); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	server, operationID, err := h.service.CreateServerAsync(r.Context(), cmd.Actor.UserID, cmd.Actor.Scope.ProjectID, cmd.Request)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusAccepted, compute.CreateServerResponseAsync{
		ServerID:    server.ID,
		OperationID: operationID,
		Status:      server.Status,
	})
}

func (h *Handler) handleGetServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	server, err := h.service.GetServer(r.Context(), serverID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]compute.Server{"server": server})
}

func (h *Handler) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var cmd struct {
		Actor authActor `json:"actor"`
	}
	if err := httpx.DecodeJSON(r, &cmd); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	resourceTask, err := h.service.DeleteServer(r.Context(), cmd.Actor.UserID, serverID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]task.Task{"task": resourceTask})
}

func (h *Handler) handleAttachVolume(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var cmd domain.AttachVolumeCommand
	if err := httpx.DecodeJSON(r, &cmd); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	server, resourceTask, err := h.service.AttachVolume(r.Context(), cmd.Actor.UserID, serverID, cmd.Request.VolumeID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, compute.CreateServerResponse{Server: server, Task: resourceTask})
}

func (h *Handler) handleDetachVolume(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	var cmd domain.AttachVolumeCommand
	if err := httpx.DecodeJSON(r, &cmd); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	server, resourceTask, err := h.service.DetachVolume(r.Context(), cmd.Actor.UserID, serverID, cmd.Request.VolumeID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, compute.CreateServerResponse{Server: server, Task: resourceTask})
}

func (h *Handler) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	resourceTask, err := h.service.GetTask(r.Context(), taskID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]task.Task{"task": resourceTask})
}

type authActor struct {
	UserID string `json:"user_id"`
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrNetworkBackendUnavailable):
		apierror.Write(w, http.StatusServiceUnavailable, "backend_unavailable", "network backend unavailable")
	case errors.Is(err, application.ErrNetworkNotFound):
		apierror.Write(w, http.StatusNotFound, "network_not_found", "network not found")
	case errors.Is(err, application.ErrImageNotFound):
		apierror.Write(w, http.StatusNotFound, "image_not_found", "image not found")
	case errors.Is(err, application.ErrUnknownFlavor):
		apierror.Write(w, http.StatusBadRequest, "unknown_flavor", "unknown flavor")
	case errors.Is(err, application.ErrNoValidHost):
		apierror.Write(w, http.StatusConflict, "no_valid_host", "no valid host")
	case errors.Is(err, ports.ErrQuotaExceeded):
		apierror.Write(w, http.StatusTooManyRequests, "quota_exceeded", "project quota exceeded")
	case errors.Is(err, application.ErrServerNotFound):
		apierror.Write(w, http.StatusNotFound, "server_not_found", "server not found")
	case errors.Is(err, application.ErrTaskNotFound):
		apierror.Write(w, http.StatusNotFound, "task_not_found", "task not found")
	case errors.Is(err, application.ErrVolumeBackendUnavailable):
		apierror.Write(w, http.StatusServiceUnavailable, "volume_backend_unavailable", "volume backend unavailable")
	case errors.Is(err, application.ErrVolumeNotFound):
		apierror.Write(w, http.StatusNotFound, "volume_not_found", "volume not found")
	case errors.Is(err, application.ErrVolumeInUse):
		apierror.Write(w, http.StatusConflict, "volume_in_use", "volume in use")
	case errors.Is(err, application.ErrVolumeNotAttached):
		apierror.Write(w, http.StatusConflict, "volume_not_attached", "volume not attached")
	default:
		h.logger.Error("compute request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
