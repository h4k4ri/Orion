package postgres

import (
	"context"
	"embed"

	"github.com/jackc/pgx/v5/pgxpool"

	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/services/orion-image/internal/domain"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	pool   *pgxpool.Pool
	schema string
}

func (s *Store) q(t string) string {
	return s.schema + "." + t
}

func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := kitpg.ApplyMigrations(ctx, pool, "orion-image", migrationsFS, "migrations"); err != nil {
		return nil, err
	}
	return &Store{pool: pool, schema: "orion_image"}, nil
}

func (s *Store) ListImages(ctx context.Context) ([]domain.Image, error) {
	const q = `SELECT id, name, status, disk_format, container_format, visibility, architecture, min_disk_gb, size_bytes, checksum_sha256, path FROM orion_image.images`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []domain.Image
	for rows.Next() {
		var img domain.Image
		if err := rows.Scan(&img.ID, &img.Name, &img.Status, &img.DiskFormat, &img.ContainerFormat, &img.Visibility, &img.Architecture, &img.MinDiskGB, &img.SizeBytes, &img.ChecksumSHA256, &img.Path); err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	return images, rows.Err()
}

func (s *Store) GetImage(ctx context.Context, imageID string) (domain.Image, error) {
	const q = `SELECT id, name, status, disk_format, container_format, visibility, architecture, min_disk_gb, size_bytes, checksum_sha256, path FROM orion_image.images WHERE id = $1`
	var img domain.Image
	if err := s.pool.QueryRow(ctx, q, imageID).Scan(&img.ID, &img.Name, &img.Status, &img.DiskFormat, &img.ContainerFormat, &img.Visibility, &img.Architecture, &img.MinDiskGB, &img.SizeBytes, &img.ChecksumSHA256, &img.Path); err != nil {
		return domain.Image{}, err
	}
	return img, nil
}

func (s *Store) SaveImage(ctx context.Context, img domain.Image) error {
	const q = `
		INSERT INTO orion_image.images (id, name, status, disk_format, container_format, visibility, architecture, min_disk_gb, size_bytes, checksum_sha256, path)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, status = EXCLUDED.status`
	_, err := s.pool.Exec(ctx, q, img.ID, img.Name, img.Status, img.DiskFormat, img.ContainerFormat, img.Visibility, img.Architecture, img.MinDiskGB, img.SizeBytes, img.ChecksumSHA256, img.Path)
	return err
}

func (s *Store) DeleteImage(ctx context.Context, imageID string) error {
	const q = `DELETE FROM orion_image.images WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, imageID)
	return err
}
