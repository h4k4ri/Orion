package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/horizon/orion/libs/go/kit/ids"
	"github.com/horizon/orion/libs/go/kit/image"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var (
	ErrImageNotFound  = errors.New("image not found")
	ErrImageProtected = errors.New("image is protected")
	ErrImageTooLarge  = errors.New("image is too large")
)

const maxUploadBytes int64 = 20 << 30

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-image")

const (
	cirrosVersion  = "0.6.3"
	cirrosFileName = "cirros-0.6.3-x86_64-disk.img"
	cirrosBaseURL  = "https://download.cirros-cloud.net/0.6.3/"
)

type Service struct {
	storeDir string
	images   map[string]image.Image
	mu       sync.RWMutex
}

func NewService(storeDir string) (*Service, error) {
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.Chmod(storeDir, 0o755); err != nil {
		return nil, err
	}

	img, err := ensureCirros(storeDir)
	if err != nil {
		return nil, err
	}

	return &Service{
		storeDir: storeDir,
		images: map[string]image.Image{
			img.ID: img,
		},
	}, nil
}

func (s *Service) ListImages(ctx context.Context) []image.Image {
	ctx, span := tracer.Start(ctx, "image.ListImages")
	defer span.End()
	s.mu.RLock()
	defer s.mu.RUnlock()
	span.SetAttributes(attribute.Int("count", len(s.images)))

	images := make([]image.Image, 0, len(s.images))
	for _, img := range s.images {
		images = append(images, img)
	}
	return images
}

func (s *Service) GetImage(ctx context.Context, imageID string) (image.Image, error) {
	ctx, span := tracer.Start(ctx, "image.GetImage")
	defer span.End()
	span.SetAttributes(attribute.String("image_id", imageID))

	s.mu.RLock()
	defer s.mu.RUnlock()
	img, ok := s.images[imageID]
	if !ok {
		return image.Image{}, ErrImageNotFound
	}
	return img, nil
}

func (s *Service) CreateImage(ctx context.Context, metadata image.Image) (image.Image, error) {
	if strings.TrimSpace(metadata.Name) == "" {
		return image.Image{}, errors.New("image name is required")
	}
	metadata.ID = ids.New("img")
	metadata.Status = "queued"
	metadata.Path = ""
	if metadata.Visibility == "" {
		metadata.Visibility = "private"
	}
	s.mu.Lock()
	s.images[metadata.ID] = metadata
	s.mu.Unlock()
	return metadata, nil
}

func (s *Service) UploadImage(ctx context.Context, imageID string, source io.Reader) (image.Image, error) {
	s.mu.RLock()
	metadata, ok := s.images[imageID]
	s.mu.RUnlock()
	if !ok {
		return image.Image{}, ErrImageNotFound
	}
	if imageID == "img_cirros_0_6_3_x86_64" {
		return image.Image{}, ErrImageProtected
	}
	tmpPath := filepath.Join(s.storeDir, "."+imageID+".upload")
	targetPath := filepath.Join(s.storeDir, imageID+".img")
	if filepath.Base(tmpPath) != "."+imageID+".upload" {
		return image.Image{}, errors.New("invalid image id")
	}
	out, err := os.Create(tmpPath)
	if err != nil {
		return image.Image{}, err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hasher), io.LimitReader(source, maxUploadBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return image.Image{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return image.Image{}, closeErr
	}
	if written > maxUploadBytes {
		_ = os.Remove(tmpPath)
		return image.Image{}, ErrImageTooLarge
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		_ = os.Remove(tmpPath)
		return image.Image{}, err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return image.Image{}, err
	}
	metadata.Path = targetPath
	metadata.SizeBytes = written
	metadata.ChecksumSHA256 = fmt.Sprintf("%x", hasher.Sum(nil))
	metadata.Status = "active"
	s.mu.Lock()
	metadata = imageOrExisting(s.images, imageID, metadata)
	s.images[imageID] = metadata
	s.mu.Unlock()
	return metadata, nil
}

func imageOrExisting(images map[string]image.Image, id string, updated image.Image) image.Image {
	if current, ok := images[id]; ok {
		if updated.Name == "" {
			updated.Name = current.Name
		}
		if updated.DiskFormat == "" {
			updated.DiskFormat = current.DiskFormat
		}
		if updated.ContainerFormat == "" {
			updated.ContainerFormat = current.ContainerFormat
		}
		if updated.Visibility == "" {
			updated.Visibility = current.Visibility
		}
		if updated.Architecture == "" {
			updated.Architecture = current.Architecture
		}
	}
	return updated
}

func (s *Service) DeleteImage(ctx context.Context, imageID string) error {
	if imageID == "img_cirros_0_6_3_x86_64" {
		return ErrImageProtected
	}
	s.mu.Lock()
	img, ok := s.images[imageID]
	if !ok {
		s.mu.Unlock()
		return ErrImageNotFound
	}
	delete(s.images, imageID)
	s.mu.Unlock()
	if img.Path != "" {
		_ = os.Remove(img.Path)
	}
	return nil
}

func ensureCirros(storeDir string) (image.Image, error) {
	imageURL := cirrosBaseURL + cirrosFileName
	checksumURL := cirrosBaseURL + "SHA256SUMS"
	targetPath := filepath.Join(storeDir, cirrosFileName)

	expectedChecksum, err := fetchExpectedChecksum(checksumURL, cirrosFileName)
	if err != nil {
		return image.Image{}, err
	}

	if err := ensureDownloaded(targetPath, imageURL, expectedChecksum); err != nil {
		return image.Image{}, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return image.Image{}, err
	}

	return image.Image{
		ID:              "img_cirros_0_6_3_x86_64",
		Name:            "CirrOS 0.6.3 x86_64",
		Status:          "active",
		DiskFormat:      "qcow2",
		ContainerFormat: "bare",
		Visibility:      "public",
		Architecture:    "x86_64",
		MinDiskGB:       1,
		SizeBytes:       info.Size(),
		ChecksumSHA256:  expectedChecksum,
		Path:            targetPath,
	}, nil
}

func fetchExpectedChecksum(url, fileName string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum fetch failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == fileName {
			return fields[0], nil
		}
	}

	return "", fmt.Errorf("checksum for %s not found", fileName)
}

func ensureDownloaded(targetPath, sourceURL, expectedChecksum string) error {
	if _, err := os.Stat(targetPath); err == nil {
		actual, err := sha256File(targetPath)
		if err == nil && actual == expectedChecksum {
			return os.Chmod(targetPath, 0o644)
		}
	}

	tmpPath := targetPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get(sourceURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("image download failed with status %d", resp.StatusCode)
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, hasher), resp.Body); err != nil {
		return err
	}

	actual := fmt.Sprintf("%x", hasher.Sum(nil))
	if actual != expectedChecksum {
		return fmt.Errorf("checksum mismatch: got %s want %s", actual, expectedChecksum)
	}

	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, targetPath)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
