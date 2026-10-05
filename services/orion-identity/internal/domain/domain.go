package domain

import (
	"time"

	"github.com/horizon/orion/libs/go/kit/authn"
)

type User struct {
	ID       string
	Username string
	Password string
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateProjectRequest struct {
	Name string `json:"name"`
}

type ProjectRoleAssignment struct {
	UserID    string     `json:"user_id"`
	ProjectID string     `json:"project_id"`
	Role      authn.Role `json:"role"`
}

type AuthenticateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Scope    struct {
		Type      authn.ScopeType `json:"type"`
		ProjectID string          `json:"project_id,omitempty"`
	} `json:"scope"`
}

type AuthenticateResponse struct {
	Token authn.Token `json:"token"`
}

type ValidateResponse struct {
	Token authn.Token `json:"token"`
}

type BootstrapData struct {
	UserRolesByProject map[string][]authn.Role
	SystemRoles        []authn.Role
	Projects           map[string]Project
}

type TokenRecord struct {
	Token authn.Token
}

func (t TokenRecord) Expired(now time.Time) bool {
	return now.After(t.Token.ExpiresAt)
}
