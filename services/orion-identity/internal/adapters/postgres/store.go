package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/horizon/orion/libs/go/kit/authn"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/services/orion-identity/internal/domain"
	"github.com/horizon/orion/services/orion-identity/internal/ports"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store is the PostgreSQL implementation of ports.Store.
type Store struct {
	pool   *pgxpool.Pool
	schema string
}

type AuditRecord struct {
	ActorID      string
	ProjectID    string
	ResourceType string
	ResourceID   string
	Operation    string
	RequestID    string
	OperationID  string
	SourceIP     string
	Result       string
	Metadata     []byte
	BeforeState  []byte
	AfterState   []byte
}

func (s *Store) q(t string) string {
	return s.schema + "." + t
}

// New creates the schema (idempotent) and returns a ready Store.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := kitpg.ApplyMigrations(ctx, pool, "orion-identity", migrationsFS, "migrations"); err != nil {
		return nil, err
	}
	return &Store{pool: pool, schema: "orion_identity"}, nil
}

// FindUserByUsername looks up a user + their bootstrap data by username.
func (s *Store) FindUserByUsername(ctx context.Context, username string) (domain.User, domain.BootstrapData, bool) {
	const q = `SELECT id, username, password FROM orion_identity.identity_users WHERE username = $1`
	var u domain.User
	if err := s.pool.QueryRow(ctx, q, username).Scan(&u.ID, &u.Username, &u.Password); err != nil {
		return domain.User{}, domain.BootstrapData{}, false
	}

	// load project roles
	const qPR = `
		SELECT upr.project_id, upr.role, p.name
		FROM orion_identity.identity_user_project_roles upr
		JOIN orion_identity.identity_projects p ON p.id = upr.project_id
		WHERE upr.user_id = $1`
	rows, err := s.pool.Query(ctx, qPR, u.ID)
	if err != nil {
		return domain.User{}, domain.BootstrapData{}, false
	}
	defer rows.Close()

	rolesByProject := map[string][]authn.Role{}
	projects := map[string]domain.Project{}
	for rows.Next() {
		var projectID, role, projectName string
		if err := rows.Scan(&projectID, &role, &projectName); err != nil {
			return domain.User{}, domain.BootstrapData{}, false
		}
		rolesByProject[projectID] = append(rolesByProject[projectID], authn.Role(role))
		projects[projectID] = domain.Project{ID: projectID, Name: projectName}
	}
	if rows.Err() != nil {
		return domain.User{}, domain.BootstrapData{}, false
	}

	// load system roles
	const qSR = `SELECT role FROM orion_identity.identity_user_system_roles WHERE user_id = $1`
	sysRows, err := s.pool.Query(ctx, qSR, u.ID)
	if err != nil {
		return domain.User{}, domain.BootstrapData{}, false
	}
	defer sysRows.Close()

	var systemRoles []authn.Role
	for sysRows.Next() {
		var role string
		if err := sysRows.Scan(&role); err != nil {
			return domain.User{}, domain.BootstrapData{}, false
		}
		systemRoles = append(systemRoles, authn.Role(role))
	}

	return u, domain.BootstrapData{
		UserRolesByProject: rolesByProject,
		SystemRoles:        systemRoles,
		Projects:           projects,
	}, true
}

// SaveToken persists a token record.
func (s *Store) SaveToken(ctx context.Context, record domain.TokenRecord) error {
	payload, err := json.Marshal(record.Token)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO orion_identity.identity_tokens (value, user_id, payload, expires_at, issued_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (value) DO UPDATE SET payload = EXCLUDED.payload, expires_at = EXCLUDED.expires_at`
	_, err = s.pool.Exec(ctx, q,
		record.Token.Value,
		record.Token.Actor.UserID,
		payload,
		record.Token.ExpiresAt,
		record.Token.IssuedAt,
	)
	return err
}

// FindToken loads a token by its value.
func (s *Store) FindToken(ctx context.Context, value string) (domain.TokenRecord, bool) {
	const q = `SELECT payload, expires_at FROM orion_identity.identity_tokens WHERE value = $1`
	var payload []byte
	var expiresAt time.Time
	if err := s.pool.QueryRow(ctx, q, value).Scan(&payload, &expiresAt); err != nil {
		return domain.TokenRecord{}, false
	}
	if time.Now().UTC().After(expiresAt) {
		return domain.TokenRecord{}, false
	}
	var token authn.Token
	if err := json.Unmarshal(payload, &token); err != nil {
		return domain.TokenRecord{}, false
	}
	return domain.TokenRecord{Token: token}, true
}

func (s *Store) AppendAudit(ctx context.Context, record AuditRecord) error {
	if len(record.Metadata) == 0 {
		record.Metadata = []byte("{}")
	}
	if len(record.BeforeState) == 0 {
		record.BeforeState = []byte("{}")
	}
	if len(record.AfterState) == 0 {
		record.AfterState = []byte("{}")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO orion_identity.audit_log
		(id, actor_id, project_id, action, resource_type, resource_id, request_id, operation_id, source_ip, result, metadata, before_state, after_state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		fmt.Sprintf("audit_%d", time.Now().UnixNano()), record.ActorID, record.ProjectID,
		record.Operation, record.ResourceType, record.ResourceID, record.RequestID,
		record.OperationID, record.SourceIP, record.Result, json.RawMessage(record.Metadata),
		json.RawMessage(record.BeforeState), json.RawMessage(record.AfterState))
	return err
}

func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, username, password FROM orion_identity.identity_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.User, 0)
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(&user.ID, &user.Username, &user.Password); err != nil {
			return nil, err
		}
		items = append(items, user)
	}
	return items, rows.Err()
}

func (s *Store) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx, `SELECT id, username, password FROM orion_identity.identity_users WHERE id=$1`, id).Scan(&user.ID, &user.Username, &user.Password)
	if err == pgx.ErrNoRows {
		return domain.User{}, ports.ErrUserRecordNotFound
	}
	return user, err
}

func (s *Store) SaveUser(ctx context.Context, user domain.User) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_identity.identity_users(id, username, password) VALUES($1,$2,$3)`, user.ID, user.Username, user.Password)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM orion_identity.identity_users WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ports.ErrUserRecordNotFound
	}
	return nil
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM orion_identity.identity_projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Project, 0)
	for rows.Next() {
		var project domain.Project
		if err := rows.Scan(&project.ID, &project.Name); err != nil {
			return nil, err
		}
		items = append(items, project)
	}
	return items, rows.Err()
}

func (s *Store) GetProject(ctx context.Context, id string) (domain.Project, error) {
	var project domain.Project
	err := s.pool.QueryRow(ctx, `SELECT id, name FROM orion_identity.identity_projects WHERE id=$1`, id).Scan(&project.ID, &project.Name)
	if err == pgx.ErrNoRows {
		return domain.Project{}, ports.ErrProjectRecordNotFound
	}
	return project, err
}

func (s *Store) SaveProject(ctx context.Context, project domain.Project) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_identity.identity_projects(id, name) VALUES($1,$2)`, project.ID, project.Name)
	return err
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM orion_identity.identity_projects WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ports.ErrProjectRecordNotFound
	}
	return nil
}

func (s *Store) AssignProjectRole(ctx context.Context, assignment domain.ProjectRoleAssignment) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_identity.identity_user_project_roles(user_id, project_id, role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, assignment.UserID, assignment.ProjectID, assignment.Role)
	return err
}
