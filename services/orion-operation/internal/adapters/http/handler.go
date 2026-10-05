package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/horizon/orion/services/orion-operation/internal/application"
	"github.com/horizon/orion/services/orion-operation/internal/domain"
)

type Handler struct {
	logger  *slog.Logger
	service *application.Service
	pool    *pgxpool.Pool
}

func NewHandler(logger *slog.Logger, service *application.Service, pool *pgxpool.Pool) *Handler {
	return &Handler{logger: logger, service: service, pool: pool}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", h.handleHealth)
	r.Get("/readyz", h.handleReady)

	r.Route("/operations", func(r chi.Router) {
		r.Post("/", h.handleCreateOperation)
		r.Get("/", h.handleListOperations)
		r.Get("/{id}", h.handleGetOperation)
		r.Post("/{id}/transition", h.handleTransition)
		r.Post("/{id}/fail", h.handleFail)
		r.Post("/{id}/succeed", h.handleSucceed)
	})

	return r
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil || h.pool.Ping(context.Background()) != nil {
		http.Error(w, "db not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (h *Handler) handleCreateOperation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID            string `json:"id"`
		ResourceType  string `json:"resource_type"`
		ResourceID    string `json:"resource_id"`
		ProjectID     string `json:"project_id"`
		RequestID     string `json:"request_id"`
		OperationType string `json:"operation_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.ID == "" || req.ResourceType == "" || req.ResourceID == "" || req.ProjectID == "" || req.OperationType == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	op, err := h.service.CreateOperation(r.Context(), domain.CreateOperationRequest{
		ID:            req.ID,
		ResourceType:  req.ResourceType,
		ResourceID:    req.ResourceID,
		ProjectID:     req.ProjectID,
		RequestID:     req.RequestID,
		OperationType: req.OperationType,
	})
	if err != nil {
		h.logger.Error("create operation failed", "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(op)
}

func (h *Handler) handleGetOperation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	op, err := h.service.GetOperation(r.Context(), id)
	if errors.Is(err, application.ErrOperationNotFound) {
		http.Error(w, "operation not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("get operation failed", "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(op)
}

func (h *Handler) handleListOperations(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		http.Error(w, "project_id query parameter required", http.StatusBadRequest)
		return
	}

	ops, err := h.service.ListOperations(r.Context(), projectID)
	if err != nil {
		h.logger.Error("list operations failed", "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"operations": ops,
		"count":      len(ops),
	})
}

func (h *Handler) handleTransition(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		State   string `json:"state"`
		Step    string `json:"step"`
		Payload []byte `json:"payload,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	err := h.service.TransitionTo(r.Context(), id, domain.OperationState(req.State), domain.OperationStep(req.Step), req.Payload)
	if errors.Is(err, application.ErrOperationNotFound) {
		http.Error(w, "operation not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, application.ErrInvalidTransition) {
		http.Error(w, "invalid state transition", http.StatusBadRequest)
		return
	}
	if errors.Is(err, application.ErrAlreadyTerminal) {
		http.Error(w, "operation already terminal", http.StatusConflict)
		return
	}
	if err != nil {
		h.logger.Error("transition failed", "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleFail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	err := h.service.Fail(r.Context(), id, req.ErrorCode, req.ErrorMessage)
	if errors.Is(err, application.ErrOperationNotFound) {
		http.Error(w, "operation not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, application.ErrAlreadyTerminal) {
		http.Error(w, "operation already terminal", http.StatusConflict)
		return
	}
	if err != nil {
		h.logger.Error("fail operation failed", "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleSucceed(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := h.service.Succeed(r.Context(), id)
	if errors.Is(err, application.ErrOperationNotFound) {
		http.Error(w, "operation not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, application.ErrAlreadyTerminal) {
		http.Error(w, "operation already terminal", http.StatusConflict)
		return
	}
	if err != nil {
		h.logger.Error("succeed operation failed", "error", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
