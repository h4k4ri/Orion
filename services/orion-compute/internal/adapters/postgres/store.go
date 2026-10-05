package postgres

import (
	"context"
	"embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/horizon/orion/libs/go/kit/compute"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/libs/go/kit/task"
	cports "github.com/horizon/orion/services/orion-compute/internal/ports"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store implements ports.Store using PostgreSQL.
type Store struct {
	pool   *pgxpool.Pool
	schema string
}

func (s *Store) q(t string) string {
	return s.schema + "." + t
}

// New runs the schema migration and returns a ready Store.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := kitpg.ApplyMigrations(ctx, pool, "orion-compute", migrationsFS, "migrations"); err != nil {
		return nil, err
	}
	return &Store{pool: pool, schema: "orion_compute"}, nil
}

// SaveServer upserts a compute server.
func (s *Store) SaveServer(ctx context.Context, srv compute.Server) error {
	const q = `
		INSERT INTO orion_compute.compute_servers
			(id, project_id, name, image_id, flavor, vcpus, memory_mb, disk_gb,
			 cell_id, host_id, port_ids, volume_ids, status, task_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (id) DO UPDATE SET
			project_id = EXCLUDED.project_id,
			name       = EXCLUDED.name,
			image_id   = EXCLUDED.image_id,
			flavor     = EXCLUDED.flavor,
			vcpus      = EXCLUDED.vcpus,
			memory_mb  = EXCLUDED.memory_mb,
			disk_gb    = EXCLUDED.disk_gb,
			cell_id    = EXCLUDED.cell_id,
			host_id    = EXCLUDED.host_id,
			port_ids   = EXCLUDED.port_ids,
			volume_ids = EXCLUDED.volume_ids,
			status     = EXCLUDED.status,
			task_id    = EXCLUDED.task_id,
			updated_at = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, q,
		srv.ID, srv.ProjectID, srv.Name, srv.ImageID, srv.Flavor,
		srv.VCPUs, srv.MemoryMB, srv.DiskGB,
		srv.CellID, srv.HostID,
		stringSlice(srv.PortIDs), stringSlice(srv.VolumeIDs),
		srv.Status, srv.TaskID,
		srv.CreatedAt, srv.UpdatedAt,
	)
	return err
}

// GetServer fetches a single server by ID.
func (s *Store) GetServer(ctx context.Context, id string) (compute.Server, error) {
	var q = `
		SELECT id, project_id, name, image_id, flavor, vcpus, memory_mb, disk_gb,
		       cell_id, host_id, port_ids, volume_ids, status, task_id, created_at, updated_at
		FROM ` + s.q("compute_servers") + ` WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	srv, err := scanServer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return compute.Server{}, cports.ErrServerNotFound
	}
	return srv, err
}

// ListServers returns all servers.
func (s *Store) ListServers(ctx context.Context) ([]compute.Server, error) {
	var q = `
		SELECT id, project_id, name, image_id, flavor, vcpus, memory_mb, disk_gb,
		       cell_id, host_id, port_ids, volume_ids, status, task_id, created_at, updated_at
		FROM ` + s.q("compute_servers") + ` ORDER BY created_at`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []compute.Server
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, srv)
	}
	return items, rows.Err()
}

// DeleteServer removes a server record.
func (s *Store) DeleteServer(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM `+s.q("compute_servers")+` WHERE id = $1`, id)
	return err
}

func (s *Store) AllocateProjectQuota(ctx context.Context, projectID string, instances, vcpus, ramMB int) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO orion_compute.project_quotas (project_id) VALUES ($1)
		ON CONFLICT (project_id) DO NOTHING`, projectID)
	if err != nil {
		return err
	}
	var allocated int
	err = s.pool.QueryRow(ctx, `
		UPDATE orion_compute.project_quotas
		SET instances_used = instances_used + $2,
		    vcpus_used = vcpus_used + $3,
		    ram_mb_used = ram_mb_used + $4,
		    updated_at = NOW()
		WHERE project_id = $1
		  AND instances_used + $2 <= instances_limit
		  AND vcpus_used + $3 <= vcpus_limit
		  AND ram_mb_used + $4 <= ram_mb_limit
		RETURNING 1`, projectID, instances, vcpus, ramMB).Scan(&allocated)
	if err == pgx.ErrNoRows {
		return cports.ErrQuotaExceeded
	}
	return err
}

func (s *Store) ReleaseProjectQuota(ctx context.Context, projectID string, instances, vcpus, ramMB int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orion_compute.project_quotas
		SET instances_used = GREATEST(0, instances_used - $2),
		    vcpus_used = GREATEST(0, vcpus_used - $3),
		    ram_mb_used = GREATEST(0, ram_mb_used - $4),
		    updated_at = NOW()
		WHERE project_id = $1`, projectID, instances, vcpus, ramMB)
	return err
}

func (s *Store) EnsureFinalizers(ctx context.Context, resourceType, resourceID string, finalizers []string) error {
	for _, finalizer := range finalizers {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO orion_compute.resource_finalizers(resource_type, resource_id, finalizer)
			VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, resourceType, resourceID, finalizer); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RemoveFinalizer(ctx context.Context, resourceType, resourceID, finalizer string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_compute.resource_finalizers WHERE resource_type=$1 AND resource_id=$2 AND finalizer=$3`, resourceType, resourceID, finalizer)
	return err
}

func (s *Store) ListFinalizers(ctx context.Context, resourceType, resourceID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT finalizer FROM orion_compute.resource_finalizers WHERE resource_type=$1 AND resource_id=$2 ORDER BY finalizer`, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var finalizers []string
	for rows.Next() {
		var finalizer string
		if err := rows.Scan(&finalizer); err != nil {
			return nil, err
		}
		finalizers = append(finalizers, finalizer)
	}
	return finalizers, rows.Err()
}

// SaveTask upserts a task record.
func (s *Store) CreateTaskIfAbsent(ctx context.Context, t task.Task) (task.Task, bool, error) {
	if t.RequestID == "" {
		if err := s.SaveTask(ctx, t); err != nil {
			return task.Task{}, false, err
		}
		return t, true, nil
	}

	const q = `
		INSERT INTO orion_compute.compute_tasks
			(id, kind, scope, target_ref, status, requested_by, request_id,
			 cell_id, host_id, error_code, error_message, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (kind, request_id) DO NOTHING`
	ct, err := s.pool.Exec(ctx, q,
		t.ID, t.Kind, t.Scope, t.TargetRef, string(t.Status),
		t.RequestedBy, t.RequestID,
		t.CellID, t.HostID, t.ErrorCode, t.ErrorMessage,
		t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return task.Task{}, false, err
	}
	if ct.RowsAffected() == 1 {
		return t, true, nil
	}

	existing, err := s.GetTaskByRequest(ctx, t.Kind, t.RequestID)
	if err != nil {
		return task.Task{}, false, err
	}
	return existing, false, nil
}

// SaveTask upserts a task record.
func (s *Store) SaveTask(ctx context.Context, t task.Task) error {
	const q = `
		INSERT INTO orion_compute.compute_tasks
			(id, kind, scope, target_ref, status, requested_by, request_id,
			 cell_id, host_id, error_code, error_message, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO UPDATE SET
			status        = EXCLUDED.status,
			cell_id       = EXCLUDED.cell_id,
			host_id       = EXCLUDED.host_id,
			error_code    = EXCLUDED.error_code,
			error_message = EXCLUDED.error_message,
			updated_at    = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, q,
		t.ID, t.Kind, t.Scope, t.TargetRef, string(t.Status),
		t.RequestedBy, t.RequestID,
		t.CellID, t.HostID, t.ErrorCode, t.ErrorMessage,
		t.CreatedAt, t.UpdatedAt,
	)
	return err
}

// GetTask fetches a single task by ID.
func (s *Store) GetTask(ctx context.Context, id string) (task.Task, error) {
	var q = `
		SELECT id, kind, scope, target_ref, status, requested_by, request_id,
		       cell_id, host_id, error_code, error_message, created_at, updated_at
		FROM ` + s.q("compute_tasks") + ` WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, cports.ErrTaskNotFound
	}
	return t, err
}

func (s *Store) GetTaskByRequest(ctx context.Context, kind, requestID string) (task.Task, error) {
	var q = `
		SELECT id, kind, scope, target_ref, status, requested_by, request_id,
		       cell_id, host_id, error_code, error_message, created_at, updated_at
		FROM ` + s.q("compute_tasks") + ` WHERE kind = $1 AND request_id = $2`
	row := s.pool.QueryRow(ctx, q, kind, requestID)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, cports.ErrTaskNotFound
	}
	return t, err
}

// ── helpers ──────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanServer(row scanner) (compute.Server, error) {
	var srv compute.Server
	var portIDs, volumeIDs []string
	var createdAt, updatedAt time.Time
	err := row.Scan(
		&srv.ID, &srv.ProjectID, &srv.Name, &srv.ImageID, &srv.Flavor,
		&srv.VCPUs, &srv.MemoryMB, &srv.DiskGB,
		&srv.CellID, &srv.HostID,
		&portIDs, &volumeIDs,
		&srv.Status, &srv.TaskID,
		&createdAt, &updatedAt,
	)
	srv.PortIDs = portIDs
	srv.VolumeIDs = volumeIDs
	srv.CreatedAt = createdAt
	srv.UpdatedAt = updatedAt
	return srv, err
}

func scanTask(row scanner) (task.Task, error) {
	var t task.Task
	var status string
	var createdAt, updatedAt time.Time
	err := row.Scan(
		&t.ID, &t.Kind, &t.Scope, &t.TargetRef, &status,
		&t.RequestedBy, &t.RequestID,
		&t.CellID, &t.HostID, &t.ErrorCode, &t.ErrorMessage,
		&createdAt, &updatedAt,
	)
	t.Status = task.Status(status)
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return t, err
}

func stringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
