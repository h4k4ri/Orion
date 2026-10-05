package ports

import (
	"context"
	"errors"

	"github.com/horizon/orion/libs/go/kit/image"
)

var ErrImageNotFound = errors.New("image not found")

type ImageResolver interface {
	GetImage(ctx context.Context, imageID string) (image.Image, error)
}
