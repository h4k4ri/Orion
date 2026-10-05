package ports

import (
	"context"
	"errors"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/services/orion-identity/internal/domain"
)

var (
	ErrUserRecordNotFound    = errors.New("user not found in store")
	ErrProjectRecordNotFound = errors.New("project not found in store")
)

type Store interface {
	FindUserByUsername(ctx context.Context, username string) (domain.User, domain.BootstrapData, bool)
	SaveToken(ctx context.Context, token domain.TokenRecord) error
	FindToken(ctx context.Context, value string) (domain.TokenRecord, bool)
}

type ManagementStore interface {
	ListUsers(ctx context.Context) ([]domain.User, error)
	GetUserByID(ctx context.Context, id string) (domain.User, error)
	SaveUser(ctx context.Context, user domain.User) error
	DeleteUser(ctx context.Context, id string) error
	ListProjects(ctx context.Context) ([]domain.Project, error)
	GetProject(ctx context.Context, id string) (domain.Project, error)
	SaveProject(ctx context.Context, project domain.Project) error
	DeleteProject(ctx context.Context, id string) error
	AssignProjectRole(ctx context.Context, assignment domain.ProjectRoleAssignment) error
}

type CatalogProvider interface {
	List(ctx context.Context) []authn.CatalogEntry
}
