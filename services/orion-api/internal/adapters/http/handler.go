package httpadapter

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/services/orion-api/internal/application"
	"github.com/horizon/orion/services/orion-api/internal/domain"
	"github.com/horizon/orion/services/orion-api/internal/ports"
)

type contextKey string

const actorKey contextKey = "actor"

type Handler struct {
	logger         *slog.Logger
	service        *application.Service
	tokenValidator ports.TokenValidator
	rateMu         sync.Mutex
	buckets        map[string]*projectBucket
	ratePerSecond  float64
	rateBurst      float64
}

type projectBucket struct {
	tokens float64
	last   time.Time
}

func NewHandler(logger *slog.Logger, service *application.Service, tokenValidator ports.TokenValidator) *Handler {
	return &Handler{
		logger:         logger,
		service:        service,
		tokenValidator: tokenValidator,
		buckets:        make(map[string]*projectBucket),
		ratePerSecond:  envFloat("ORION_API_RATE_LIMIT_PER_SECOND", 10),
		rateBurst:      envFloat("ORION_API_RATE_LIMIT_BURST", 20),
	}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestID)

	r.Get("/healthz", h.handleHealth)
	r.Get("/readyz", h.handleReady)

	r.Group(func(r chi.Router) {
		r.Use(h.withAuth)
		r.Use(h.rateLimit)
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
		r.Route("/v1/operations", func(r chi.Router) {
			r.Get("/{operationID}", h.handleGetOperation)
		})
		r.Route("/v1/volumes", func(r chi.Router) {
			r.Get("/", h.handleListVolumes)
			r.Post("/", h.handleCreateVolume)
			r.Get("/{volumeID}", h.handleGetVolume)
			r.Delete("/{volumeID}", h.handleDeleteVolume)
		})
	})

	return r
}

func (h *Handler) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		key := actor.Scope.ProjectID
		if key == "" {
			key = "system:" + actor.UserID
		}
		now := time.Now()
		h.rateMu.Lock()
		bucket := h.buckets[key]
		if bucket == nil {
			bucket = &projectBucket{tokens: h.rateBurst, last: now}
			h.buckets[key] = bucket
		}
		elapsed := now.Sub(bucket.last).Seconds()
		bucket.tokens = minFloat(h.rateBurst, bucket.tokens+elapsed*h.ratePerSecond)
		bucket.last = now
		allowed := bucket.tokens >= 1
		if allowed {
			bucket.tokens--
		}
		h.rateMu.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "1")
			apierror.Write(w, http.StatusTooManyRequests, "rate_limited", "project rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenValue := r.Header.Get("Authorization")
		if len(tokenValue) > 7 && tokenValue[:7] == "Bearer " {
			tokenValue = tokenValue[7:]
		}
		if tokenValue == "" {
			apierror.Write(w, http.StatusUnauthorized, "missing_token", "missing bearer token")
			return
		}

		token, err := h.tokenValidator.Validate(r.Context(), tokenValue)
		if err != nil {
			h.logger.Warn("token validation failed", "error", err.Error())
			apierror.Write(w, http.StatusUnauthorized, "invalid_token", "invalid token")
			return
		}

		ctx := context.WithValue(r.Context(), actorKey, token.Actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func actorFromContext(ctx context.Context) (authn.Actor, bool) {
	actor, ok := ctx.Value(actorKey).(authn.Actor)
	return actor, ok
}

func (h *Handler) handleListServers(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}
	servers, err := h.service.ListServers(r.Context(), actor)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]domain.Server{"servers": servers})
}

func (h *Handler) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	var req domain.CreateServerRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		if len(key) > 128 || strings.ContainsAny(key, "\r\n") {
			apierror.Write(w, http.StatusBadRequest, "invalid_idempotency_key", "invalid idempotency key")
			return
		}
		requestID := actor.Scope.ProjectID + ":idem:" + key
		r = r.WithContext(httpx.WithRequestID(r.Context(), requestID))
		w.Header().Set("Idempotency-Key", key)
	}

	response, err := h.service.CreateServerAsync(r.Context(), actor, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusAccepted, response)
}

func (h *Handler) handleGetOperation(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}
	operation, err := h.service.GetOperation(r.Context(), actor, chi.URLParam(r, "operationID"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, operation)
}

func (h *Handler) handleGetServer(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	serverID := chi.URLParam(r, "serverID")
	server, err := h.service.GetServer(r.Context(), actor, serverID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]domain.Server{"server": server})
}

func (h *Handler) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	serverID := chi.URLParam(r, "serverID")
	resourceTask, err := h.service.DeleteServer(r.Context(), actor, serverID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"task": resourceTask})
}

func (h *Handler) handleAttachVolume(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	serverID := chi.URLParam(r, "serverID")
	var req compute.AttachVolumeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	server, resourceTask, err := h.service.AttachVolume(r.Context(), actor, serverID, req.VolumeID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, domain.CreateServerResponse{Server: server, Task: resourceTask})
}

func (h *Handler) handleDetachVolume(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	serverID := chi.URLParam(r, "serverID")
	var req compute.AttachVolumeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	server, resourceTask, err := h.service.DetachVolume(r.Context(), actor, serverID, req.VolumeID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, domain.CreateServerResponse{Server: server, Task: resourceTask})
}

func (h *Handler) handleGetTask(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	taskID := chi.URLParam(r, "taskID")
	resourceTask, err := h.service.GetTask(r.Context(), actor, taskID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"task": resourceTask})
}

func (h *Handler) handleListVolumes(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}
	volumes, err := h.service.ListVolumes(r.Context(), actor)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]domain.Volume{"volumes": volumes})
}

func (h *Handler) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	var req domain.CreateVolumeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	volume, err := h.service.CreateVolume(r.Context(), actor, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]domain.Volume{"volume": volume})
}

func (h *Handler) handleGetVolume(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	volumeID := chi.URLParam(r, "volumeID")
	volume, err := h.service.GetVolume(r.Context(), actor, volumeID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]domain.Volume{"volume": volume})
}

func (h *Handler) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromContext(r.Context())
	if !ok {
		apierror.Write(w, http.StatusUnauthorized, "missing_actor", "missing actor context")
		return
	}

	volumeID := chi.URLParam(r, "volumeID")
	if err := h.service.DeleteVolume(r.Context(), actor, volumeID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrProjectScopeRequired):
		apierror.Write(w, http.StatusForbidden, "project_scope_required", "project-scoped token required")
	case errors.Is(err, application.ErrForbidden):
		apierror.Write(w, http.StatusForbidden, "forbidden", "forbidden")
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
	case errors.Is(err, application.ErrQuotaExceeded):
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
		h.logger.Error("api request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
