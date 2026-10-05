package application

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/ids"
	"github.com/horizon/orion/services/orion-identity/internal/domain"
	"github.com/horizon/orion/services/orion-identity/internal/ports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidScope       = errors.New("invalid scope")
	ErrTokenNotFound      = errors.New("token not found")
	ErrTokenExpired       = errors.New("token expired")
	ErrUserNotFound       = errors.New("user not found")
	ErrProjectNotFound    = errors.New("project not found")
	ErrInvalidRole        = errors.New("invalid role")
)

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-identity")

type Service struct {
	store          ports.Store
	catalog        ports.CatalogProvider
	tokenTTL       time.Duration
	currentTimeUTC func() time.Time
}

func NewService(store ports.Store, catalog ports.CatalogProvider, tokenTTL time.Duration) *Service {
	return &Service{
		store:    store,
		catalog:  catalog,
		tokenTTL: tokenTTL,
		currentTimeUTC: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *Service) Authenticate(ctx context.Context, req domain.AuthenticateRequest) (authn.Token, error) {
	ctx, span := tracer.Start(ctx, "identity.Authenticate")
	defer span.End()
	span.SetAttributes(attribute.String("username", req.Username))

	user, data, ok := s.store.FindUserByUsername(ctx, req.Username)
	if !ok || !verifyPassword(user.Password, req.Password) {
		return authn.Token{}, ErrInvalidCredentials
	}

	scope, roles, err := buildScope(req.Scope.Type, req.Scope.ProjectID, data)
	if err != nil {
		return authn.Token{}, err
	}

	now := s.currentTimeUTC()
	token := authn.Token{
		Value: ids.New("tok"),
		Actor: authn.Actor{
			UserID:   user.ID,
			Username: user.Username,
			Roles:    roles,
			Scope:    scope,
			Expires:  now.Add(s.tokenTTL),
		},
		Catalog:   s.catalog.List(ctx),
		IssuedAt:  now,
		ExpiresAt: now.Add(s.tokenTTL),
	}

	if err := s.store.SaveToken(ctx, domain.TokenRecord{Token: token}); err != nil {
		return authn.Token{}, err
	}

	span.SetAttributes(
		attribute.String("user_id", user.ID),
		attribute.String("token_id", token.Value),
	)
	return token, nil
}

func (s *Service) Validate(ctx context.Context, value string) (authn.Token, error) {
	ctx, span := tracer.Start(ctx, "identity.Validate")
	defer span.End()

	record, ok := s.store.FindToken(ctx, value)
	if !ok {
		return authn.Token{}, ErrTokenNotFound
	}

	now := s.currentTimeUTC()
	if record.Expired(now) {
		return authn.Token{}, ErrTokenExpired
	}

	span.SetAttributes(attribute.String("user_id", record.Token.Actor.UserID))
	return record.Token, nil
}

func (s *Service) GetBootstrapData(ctx context.Context, username, password string) (domain.BootstrapData, string, error) {
	ctx, span := tracer.Start(ctx, "identity.GetBootstrapData")
	defer span.End()
	span.SetAttributes(attribute.String("username", username))

	user, data, ok := s.store.FindUserByUsername(ctx, username)
	if !ok || !verifyPassword(user.Password, password) {
		return domain.BootstrapData{}, "", ErrInvalidCredentials
	}

	span.SetAttributes(attribute.String("user_id", user.ID))
	return data, user.ID, nil
}

func (s *Service) ListUsers(ctx context.Context) ([]domain.User, error) {
	store, ok := s.store.(ports.ManagementStore)
	if !ok {
		return nil, errors.New("identity management is not supported by the configured store")
	}
	return store.ListUsers(ctx)
}

func (s *Service) CreateUser(ctx context.Context, req domain.CreateUserRequest) (domain.User, error) {
	store, ok := s.store.(ports.ManagementStore)
	if !ok {
		return domain.User{}, errors.New("identity management is not supported by the configured store")
	}
	if req.Username == "" || req.Password == "" {
		return domain.User{}, errors.New("username and password are required")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, err
	}
	user := domain.User{ID: ids.New("usr"), Username: req.Username, Password: string(hashed)}
	if err := store.SaveUser(ctx, user); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func verifyPassword(stored, supplied string) bool {
	if strings.HasPrefix(stored, "$2a$") || strings.HasPrefix(stored, "$2b$") || strings.HasPrefix(stored, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(supplied)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(supplied)) == 1
}

func (s *Service) DeleteUser(ctx context.Context, id string) error {
	store, ok := s.store.(ports.ManagementStore)
	if !ok {
		return errors.New("identity management is not supported by the configured store")
	}
	if _, err := store.GetUserByID(ctx, id); err != nil {
		if errors.Is(err, ports.ErrUserRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	return store.DeleteUser(ctx, id)
}

func (s *Service) ListProjects(ctx context.Context) ([]domain.Project, error) {
	store, ok := s.store.(ports.ManagementStore)
	if !ok {
		return nil, errors.New("identity management is not supported by the configured store")
	}
	return store.ListProjects(ctx)
}

func (s *Service) CreateProject(ctx context.Context, req domain.CreateProjectRequest) (domain.Project, error) {
	store, ok := s.store.(ports.ManagementStore)
	if !ok {
		return domain.Project{}, errors.New("identity management is not supported by the configured store")
	}
	if req.Name == "" {
		return domain.Project{}, errors.New("name is required")
	}
	project := domain.Project{ID: ids.New("proj"), Name: req.Name}
	if err := store.SaveProject(ctx, project); err != nil {
		return domain.Project{}, err
	}
	return project, nil
}

func (s *Service) DeleteProject(ctx context.Context, id string) error {
	store, ok := s.store.(ports.ManagementStore)
	if !ok {
		return errors.New("identity management is not supported by the configured store")
	}
	if _, err := store.GetProject(ctx, id); err != nil {
		if errors.Is(err, ports.ErrProjectRecordNotFound) {
			return ErrProjectNotFound
		}
		return err
	}
	return store.DeleteProject(ctx, id)
}

func (s *Service) AssignProjectRole(ctx context.Context, assignment domain.ProjectRoleAssignment) error {
	store, ok := s.store.(ports.ManagementStore)
	if !ok {
		return errors.New("identity management is not supported by the configured store")
	}
	if assignment.Role != authn.RoleAdmin && assignment.Role != authn.RoleMember && assignment.Role != authn.RoleReader {
		return ErrInvalidRole
	}
	if _, err := store.GetUserByID(ctx, assignment.UserID); err != nil {
		if errors.Is(err, ports.ErrUserRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if _, err := store.GetProject(ctx, assignment.ProjectID); err != nil {
		if errors.Is(err, ports.ErrProjectRecordNotFound) {
			return ErrProjectNotFound
		}
		return err
	}
	return store.AssignProjectRole(ctx, assignment)
}

func buildScope(scopeType authn.ScopeType, projectID string, data domain.BootstrapData) (authn.Scope, []authn.Role, error) {
	switch scopeType {
	case authn.ScopeTypeProject:
		roles, ok := data.UserRolesByProject[projectID]
		if !ok {
			return authn.Scope{}, nil, ErrInvalidScope
		}
		return authn.Scope{
			Type:      authn.ScopeTypeProject,
			ProjectID: projectID,
		}, roles, nil
	case authn.ScopeTypeSystem:
		if len(data.SystemRoles) == 0 {
			return authn.Scope{}, nil, ErrInvalidScope
		}
		return authn.Scope{
			Type: authn.ScopeTypeSystem,
		}, data.SystemRoles, nil
	default:
		return authn.Scope{}, nil, ErrInvalidScope
	}
}
