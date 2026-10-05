package httpadapter

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/services/orion-identity/internal/application"
	"github.com/horizon/orion/services/orion-identity/internal/domain"
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

	r.Route("/v1/auth", func(r chi.Router) {
		r.Post("/tokens", h.handleAuthenticate)
		r.Get("/tokens", h.handleValidate)
		r.Post("/login", h.handleLogin)
	})
	r.Route("/v1/users", func(r chi.Router) {
		r.Get("/", h.handleListUsers)
		r.Post("/", h.handleCreateUser)
		r.Delete("/{userID}", h.handleDeleteUser)
	})
	r.Route("/v1/projects", func(r chi.Router) {
		r.Get("/", h.handleListProjects)
		r.Post("/", h.handleCreateProject)
		r.Delete("/{projectID}", h.handleDeleteProject)
		r.Post("/{projectID}/roles", h.handleAssignProjectRole)
	})

	return r
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	var req domain.AuthenticateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	token, err := h.service.Authenticate(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.Header().Set("X-Subject-Token", token.Value)
	httpx.WriteJSON(w, http.StatusCreated, domain.AuthenticateResponse{Token: token})
}

func (h *Handler) handleValidate(w http.ResponseWriter, r *http.Request) {
	subjectToken := r.Header.Get("X-Subject-Token")
	if subjectToken == "" {
		apierror.Write(w, http.StatusBadRequest, "missing_subject_token", "missing X-Subject-Token header")
		return
	}

	token, err := h.service.Validate(r.Context(), subjectToken)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, domain.ValidateResponse{Token: token})
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	data, _, err := h.service.GetBootstrapData(r.Context(), req.Username, req.Password)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	projects := make([]domain.Project, 0, len(data.Projects))
	for _, p := range data.Projects {
		projects = append(projects, p)
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.requireSystemAdmin(w, r) {
		return
	}
	users, err := h.service.ListUsers(r.Context())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	items := make([]map[string]string, 0, len(users))
	for _, user := range users {
		items = append(items, map[string]string{"id": user.ID, "username": user.Username})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": items, "total": len(items)})
}

func (h *Handler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireSystemAdmin(w, r) {
		return
	}
	var req domain.CreateUserRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	user, err := h.service.CreateUser(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]map[string]string{"user": {"id": user.ID, "username": user.Username}})
}

func (h *Handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !h.requireSystemAdmin(w, r) {
		return
	}
	if err := h.service.DeleteUser(r.Context(), chi.URLParam(r, "userID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if !h.requireSystemAdmin(w, r) {
		return
	}
	projects, err := h.service.ListProjects(r.Context())
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"projects": projects, "total": len(projects)})
}

func (h *Handler) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	if !h.requireSystemAdmin(w, r) {
		return
	}
	var req domain.CreateProjectRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	project, err := h.service.CreateProject(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]domain.Project{"project": project})
}

func (h *Handler) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if !h.requireSystemAdmin(w, r) {
		return
	}
	if err := h.service.DeleteProject(r.Context(), chi.URLParam(r, "projectID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleAssignProjectRole(w http.ResponseWriter, r *http.Request) {
	if !h.requireSystemAdmin(w, r) {
		return
	}
	var req struct {
		UserID string     `json:"user_id"`
		Role   authn.Role `json:"role"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if err := h.service.AssignProjectRole(r.Context(), domain.ProjectRoleAssignment{UserID: req.UserID, ProjectID: chi.URLParam(r, "projectID"), Role: req.Role}); err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"status": "assigned"})
}

func (h *Handler) requireSystemAdmin(w http.ResponseWriter, r *http.Request) bool {
	value := r.Header.Get("X-Subject-Token")
	if value == "" {
		apierror.Write(w, http.StatusUnauthorized, "missing_subject_token", "missing X-Subject-Token header")
		return false
	}
	token, err := h.service.Validate(r.Context(), value)
	if err != nil {
		h.writeServiceError(w, err)
		return false
	}
	if token.Actor.Scope.Type != authn.ScopeTypeSystem || !authn.HasRole(token.Actor.Roles, authn.RoleAdmin) {
		apierror.Write(w, http.StatusForbidden, "forbidden", "system administrator role required")
		return false
	}
	return true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidCredentials):
		apierror.Write(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	case errors.Is(err, application.ErrInvalidScope):
		apierror.Write(w, http.StatusBadRequest, "invalid_scope", "invalid scope")
	case errors.Is(err, application.ErrTokenNotFound):
		apierror.Write(w, http.StatusUnauthorized, "token_not_found", "token not found")
	case errors.Is(err, application.ErrTokenExpired):
		apierror.Write(w, http.StatusUnauthorized, "token_expired", "token expired")
	case errors.Is(err, application.ErrUserNotFound):
		apierror.Write(w, http.StatusNotFound, "user_not_found", "user not found")
	case errors.Is(err, application.ErrProjectNotFound):
		apierror.Write(w, http.StatusNotFound, "project_not_found", "project not found")
	case errors.Is(err, application.ErrInvalidRole):
		apierror.Write(w, http.StatusBadRequest, "invalid_role", "invalid role")
	default:
		h.logger.Error("identity request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
