package authn

import "time"

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleReader Role = "reader"
)

type ScopeType string

const (
	ScopeTypeProject ScopeType = "project"
	ScopeTypeSystem  ScopeType = "system"
)

type Scope struct {
	Type      ScopeType `json:"type"`
	ProjectID string    `json:"project_id,omitempty"`
}

type Actor struct {
	UserID   string    `json:"user_id"`
	Username string    `json:"username"`
	Roles    []Role    `json:"roles"`
	Scope    Scope     `json:"scope"`
	Expires  time.Time `json:"expires_at"`
}

type CatalogEntry struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	PublicURL string `json:"public_url"`
}

type Token struct {
	Value     string         `json:"value"`
	Actor     Actor          `json:"actor"`
	Catalog   []CatalogEntry `json:"catalog"`
	IssuedAt  time.Time      `json:"issued_at"`
	ExpiresAt time.Time      `json:"expires_at"`
}

func HasRole(roles []Role, wanted ...Role) bool {
	for _, role := range roles {
		for _, candidate := range wanted {
			if role == candidate {
				return true
			}
		}
	}

	return false
}
