package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/ids"
	"github.com/horizon/orion/services/orion-identity/internal/domain"
	"github.com/horizon/orion/services/orion-identity/internal/ports"
)

type Store struct {
	mu       sync.RWMutex
	users    map[string]domain.User
	data     map[string]domain.BootstrapData
	projects map[string]domain.Project
	tokens   map[string]domain.TokenRecord
}

func NewStore() *Store {
	adminUser := domain.User{
		ID:       ids.New("usr"),
		Username: "admin",
		Password: "orion-admin",
	}

	project := domain.Project{
		ID:   "proj_admin",
		Name: "admin",
	}

	return &Store{
		users: map[string]domain.User{
			adminUser.Username: adminUser,
		},
		data: map[string]domain.BootstrapData{
			adminUser.Username: {
				UserRolesByProject: map[string][]authn.Role{
					project.ID: {authn.RoleAdmin, authn.RoleMember, authn.RoleReader},
				},
				SystemRoles: []authn.Role{authn.RoleAdmin, authn.RoleMember, authn.RoleReader},
				Projects: map[string]domain.Project{
					project.ID: project,
				},
			},
		},
		projects: map[string]domain.Project{project.ID: project},
		tokens:   map[string]domain.TokenRecord{},
	}
}

func (s *Store) ListUsers(_ context.Context) ([]domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.User, 0, len(s.users))
	for _, user := range s.users {
		items = append(items, user)
	}
	return items, nil
}

func (s *Store) GetUserByID(_ context.Context, id string) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.users {
		if user.ID == id {
			return user, nil
		}
	}
	return domain.User{}, ports.ErrUserRecordNotFound
}

func (s *Store) SaveUser(_ context.Context, user domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[user.Username]; exists {
		return fmt.Errorf("username already exists")
	}
	s.users[user.Username] = user
	s.data[user.Username] = domain.BootstrapData{UserRolesByProject: map[string][]authn.Role{}, Projects: map[string]domain.Project{}}
	return nil
}

func (s *Store) DeleteUser(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for username, user := range s.users {
		if user.ID == id {
			delete(s.users, username)
			delete(s.data, username)
			return nil
		}
	}
	return ports.ErrUserRecordNotFound
}

func (s *Store) ListProjects(_ context.Context) ([]domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]domain.Project, 0, len(s.projects))
	for _, project := range s.projects {
		items = append(items, project)
	}
	return items, nil
}

func (s *Store) GetProject(_ context.Context, id string) (domain.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.projects[id]
	if !ok {
		return domain.Project{}, ports.ErrProjectRecordNotFound
	}
	return project, nil
}

func (s *Store) SaveProject(_ context.Context, project domain.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[project.ID] = project
	return nil
}

func (s *Store) DeleteProject(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.projects[id]; !ok {
		return ports.ErrProjectRecordNotFound
	}
	delete(s.projects, id)
	for username, data := range s.data {
		delete(data.Projects, id)
		delete(data.UserRolesByProject, id)
		s.data[username] = data
	}
	return nil
}

func (s *Store) AssignProjectRole(_ context.Context, assignment domain.ProjectRoleAssignment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for username, user := range s.users {
		if user.ID != assignment.UserID {
			continue
		}
		data := s.data[username]
		data.UserRolesByProject[assignment.ProjectID] = appendUniqueRole(data.UserRolesByProject[assignment.ProjectID], assignment.Role)
		if project, ok := s.projects[assignment.ProjectID]; ok {
			data.Projects[assignment.ProjectID] = project
		}
		s.data[username] = data
		return nil
	}
	return ports.ErrUserRecordNotFound
}

func appendUniqueRole(roles []authn.Role, role authn.Role) []authn.Role {
	for _, existing := range roles {
		if existing == role {
			return roles
		}
	}
	return append(roles, role)
}

func (s *Store) FindUserByUsername(_ context.Context, username string) (domain.User, domain.BootstrapData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[username]
	if !ok {
		return domain.User{}, domain.BootstrapData{}, false
	}

	return user, s.data[username], true
}

func (s *Store) SaveToken(_ context.Context, token domain.TokenRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokens[token.Token.Value] = token
	return nil
}

func (s *Store) FindToken(_ context.Context, value string) (domain.TokenRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.tokens[value]
	return record, ok
}
