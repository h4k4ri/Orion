package ports

import (
	"context"
	"errors"

	"github.com/horizon/orion/services/orion-placement/internal/domain"
)

var ErrHostRecordNotFound = errors.New("host not found in store")
var ErrResourceProviderNotFound = errors.New("resource provider not found in store")
var ErrResourceProviderConflict = errors.New("resource provider hierarchy conflict")

// PlacementStore persists host resource providers.
type PlacementStore interface {
	SaveHost(ctx context.Context, host domain.Host) error
	GetHost(ctx context.Context, hostID string) (domain.Host, error)
	ListHosts(ctx context.Context) ([]domain.Host, error)
	UpdateAllocation(ctx context.Context, req domain.AllocationUpdate) (domain.Host, error)
	CreateReservation(ctx context.Context, r domain.Reservation) error
	GetReservation(ctx context.Context, id string) (domain.Reservation, error)
	DeleteReservation(ctx context.Context, id string) error
	DeleteReservationByHostAndProject(ctx context.Context, hostID, projectID string) error
	ListReservationsByProject(ctx context.Context, projectID string) ([]domain.Reservation, error)
	CountProjectInstancesOnHost(ctx context.Context, hostID, projectID string) (int, error)
	CleanupExpiredReservations(ctx context.Context) (int64, error)
}

type ResourceProviderStore interface {
	SaveResourceProvider(ctx context.Context, provider domain.ResourceProvider) error
	GetResourceProvider(ctx context.Context, uuid string) (domain.ResourceProvider, error)
	ListResourceProviders(ctx context.Context, rootUUID string) ([]domain.ResourceProvider, error)
	DeleteResourceProvider(ctx context.Context, uuid string) error
}

var ErrConcurrentUpdate = errors.New("concurrent host update detected")
var ErrInsufficientCapacity = errors.New("insufficient host capacity")
