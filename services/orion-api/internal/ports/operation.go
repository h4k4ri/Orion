package ports

import (
	"context"

	"github.com/horizon/orion/services/orion-api/internal/domain"
)

type OperationClient interface {
	GetOperation(ctx context.Context, operationID string) (domain.Operation, error)
}
