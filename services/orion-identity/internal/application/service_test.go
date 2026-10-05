package application

import (
	"context"
	"testing"
	"time"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/services/orion-identity/internal/adapters/catalog"
	"github.com/horizon/orion/services/orion-identity/internal/adapters/memory"
	"github.com/horizon/orion/services/orion-identity/internal/domain"
)

func TestManagementCreatesHashedUserAndAssignsProjectRole(t *testing.T) {
	store := memory.NewStore()
	service := NewService(store, catalog.NewStatic(), time.Hour)

	user, err := service.CreateUser(context.Background(), domain.CreateUserRequest{Username: "alice", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if user.Password == "secret" || len(user.Password) < 20 {
		t.Fatalf("password was not hashed")
	}
	project, err := service.CreateProject(context.Background(), domain.CreateProjectRequest{Name: "engineering"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AssignProjectRole(context.Background(), domain.ProjectRoleAssignment{UserID: user.ID, ProjectID: project.ID, Role: authn.RoleMember}); err != nil {
		t.Fatal(err)
	}

	token, err := service.Authenticate(context.Background(), domain.AuthenticateRequest{Username: "alice", Password: "secret", Scope: struct {
		Type      authn.ScopeType `json:"type"`
		ProjectID string          `json:"project_id,omitempty"`
	}{Type: authn.ScopeTypeProject, ProjectID: project.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if !authn.HasRole(token.Actor.Roles, authn.RoleMember) {
		t.Fatalf("expected member role, got %#v", token.Actor.Roles)
	}
}
