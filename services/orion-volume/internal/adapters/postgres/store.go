package postgres

import (
	"context"
	"embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-volume/internal/ports"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store implements ports.VolumeStore using PostgreSQL.
type Store struct {
	pool   *pgxpool.Pool
	schema string
}

func (s *Store) q(t string) string {
	return s.schema + "." + t
}

// New runs the schema migration and returns a ready Store.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := kitpg.ApplyMigrations(ctx, pool, "orion-volume", migrationsFS, "migrations"); err != nil {
		return nil, err
	}
	return &Store{pool: pool, schema: "orion_volume"}, nil
}

func (s *Store) AllocateProjectQuota(ctx context.Context, projectID string, volumes, volumeGB int) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_volume.project_quotas(project_id) VALUES ($1) ON CONFLICT DO NOTHING`, projectID)
	if err != nil {
		return err
	}
	var allocated int
	err = s.pool.QueryRow(ctx, `UPDATE orion_volume.project_quotas SET volumes_used=volumes_used+$2, volume_gb_used=volume_gb_used+$3, updated_at=NOW() WHERE project_id=$1 AND volumes_used+$2<=volumes_limit AND volume_gb_used+$3<=volume_gb_limit RETURNING 1`, projectID, volumes, volumeGB).Scan(&allocated)
	if err == pgx.ErrNoRows {
		return ports.ErrQuotaExceeded
	}
	return err
}

func (s *Store) ReleaseProjectQuota(ctx context.Context, projectID string, volumes, volumeGB int) error {
	_, err := s.pool.Exec(ctx, `UPDATE orion_volume.project_quotas SET volumes_used=GREATEST(0, volumes_used-$2), volume_gb_used=GREATEST(0, volume_gb_used-$3), updated_at=NOW() WHERE project_id=$1`, projectID, volumes, volumeGB)
	return err
}

func (s *Store) EnsureFinalizers(ctx context.Context, resourceType, resourceID string, finalizers []string) error {
	for _, finalizer := range finalizers {
		if _, err := s.pool.Exec(ctx, `INSERT INTO orion_volume.resource_finalizers(resource_type, resource_id, finalizer) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, resourceType, resourceID, finalizer); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RemoveFinalizer(ctx context.Context, resourceType, resourceID, finalizer string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_volume.resource_finalizers WHERE resource_type=$1 AND resource_id=$2 AND finalizer=$3`, resourceType, resourceID, finalizer)
	return err
}

func (s *Store) ListFinalizers(ctx context.Context, resourceType, resourceID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT finalizer FROM orion_volume.resource_finalizers WHERE resource_type=$1 AND resource_id=$2 ORDER BY finalizer`, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var finalizer string
		if err := rows.Scan(&finalizer); err != nil {
			return nil, err
		}
		result = append(result, finalizer)
	}
	return result, rows.Err()
}

// SaveVolume upserts a volume record.
func (s *Store) SaveVolume(ctx context.Context, v volumekit.Volume) error {
	const q = `
		INSERT INTO orion_volume.volumes
			(id, project_id, name, size_gb, cell_id, host_id, status,
			 backend_type, device_path, attached_server_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET
			project_id         = EXCLUDED.project_id,
			name               = EXCLUDED.name,
			size_gb            = EXCLUDED.size_gb,
			cell_id            = EXCLUDED.cell_id,
			host_id            = EXCLUDED.host_id,
			status             = EXCLUDED.status,
			backend_type       = EXCLUDED.backend_type,
			device_path        = EXCLUDED.device_path,
			attached_server_id = EXCLUDED.attached_server_id,
			updated_at         = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, q,
		v.ID, v.ProjectID, v.Name, v.SizeGB,
		v.CellID, v.HostID, v.Status, v.BackendType,
		v.DevicePath, v.AttachedServerID,
		v.CreatedAt, v.UpdatedAt,
	)
	return err
}

// GetVolume fetches a volume by ID.
func (s *Store) GetVolume(ctx context.Context, id string) (volumekit.Volume, error) {
	const q = `
		SELECT id, project_id, name, size_gb, cell_id, host_id, status,
		       backend_type, device_path, attached_server_id, created_at, updated_at
		FROM orion_volume.volumes WHERE id = $1`
	v, err := scanVolume(s.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return volumekit.Volume{}, ports.ErrVolumeRecordNotFound
	}
	return v, err
}

// ListVolumes returns all volumes ordered by creation time.
func (s *Store) ListVolumes(ctx context.Context) ([]volumekit.Volume, error) {
	const q = `
		SELECT id, project_id, name, size_gb, cell_id, host_id, status,
		       backend_type, device_path, attached_server_id, created_at, updated_at
		FROM orion_volume.volumes ORDER BY created_at`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []volumekit.Volume
	for rows.Next() {
		v, err := scanVolume(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// DeleteVolume removes a volume record.
func (s *Store) DeleteVolume(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_volume.volumes WHERE id = $1`, id)
	return err
}

func (s *Store) SaveSnapshot(ctx context.Context, snapshot volumekit.VolumeSnapshot) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_volume.volume_snapshots(id, volume_id, project_id, name, size_gb, status, created_at, updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name, status=EXCLUDED.status, updated_at=EXCLUDED.updated_at`, snapshot.ID, snapshot.VolumeID, snapshot.ProjectID, snapshot.Name, snapshot.SizeGB, snapshot.Status, snapshot.CreatedAt, snapshot.UpdatedAt)
	return err
}

func (s *Store) GetSnapshot(ctx context.Context, id string) (volumekit.VolumeSnapshot, error) {
	var item volumekit.VolumeSnapshot
	err := s.pool.QueryRow(ctx, `SELECT id, volume_id, project_id, name, size_gb, status, created_at, updated_at FROM orion_volume.volume_snapshots WHERE id=$1`, id).Scan(&item.ID, &item.VolumeID, &item.ProjectID, &item.Name, &item.SizeGB, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return volumekit.VolumeSnapshot{}, ports.ErrSnapshotRecordNotFound
	}
	return item, err
}

func (s *Store) ListSnapshots(ctx context.Context, projectID, volumeID string) ([]volumekit.VolumeSnapshot, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, volume_id, project_id, name, size_gb, status, created_at, updated_at FROM orion_volume.volume_snapshots WHERE ($1='' OR project_id=$1) AND ($2='' OR volume_id=$2) ORDER BY created_at`, projectID, volumeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]volumekit.VolumeSnapshot, 0)
	for rows.Next() {
		var item volumekit.VolumeSnapshot
		if err := rows.Scan(&item.ID, &item.VolumeID, &item.ProjectID, &item.Name, &item.SizeGB, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteSnapshot(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_volume.volume_snapshots WHERE id=$1`, id)
	return err
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanVolume(row scanner) (volumekit.Volume, error) {
	var v volumekit.Volume
	var createdAt, updatedAt time.Time
	err := row.Scan(
		&v.ID, &v.ProjectID, &v.Name, &v.SizeGB,
		&v.CellID, &v.HostID, &v.Status, &v.BackendType,
		&v.DevicePath, &v.AttachedServerID,
		&createdAt, &updatedAt,
	)
	v.CreatedAt = createdAt
	v.UpdatedAt = updatedAt
	return v, err
}
