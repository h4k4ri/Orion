package catalog

import (
	"context"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/config"
)

type Static struct{}

func NewStatic() *Static {
	return &Static{}
}

func (s *Static) List(context.Context) []authn.CatalogEntry {
	return []authn.CatalogEntry{
		{
			Name:      "identity",
			Type:      "identity",
			PublicURL: config.String("ORION_IDENTITY_PUBLIC_URL", "http://localhost:8081"),
		},
		{
			Name:      "api",
			Type:      "api",
			PublicURL: config.String("ORION_API_PUBLIC_URL", "http://localhost:8080"),
		},
		{
			Name:      "placement",
			Type:      "placement",
			PublicURL: config.String("ORION_PLACEMENT_PUBLIC_URL", "http://localhost:8082"),
		},
		{
			Name:      "compute",
			Type:      "compute",
			PublicURL: config.String("ORION_COMPUTE_PUBLIC_URL", "http://localhost:8083"),
		},
		{
			Name:      "image",
			Type:      "image",
			PublicURL: config.String("ORION_IMAGE_PUBLIC_URL", "http://localhost:8085"),
		},
		{
			Name:      "network",
			Type:      "network",
			PublicURL: config.String("ORION_NETWORK_PUBLIC_URL", "http://localhost:8086"),
		},
		{
			Name:      "volume",
			Type:      "volume",
			PublicURL: config.String("ORION_VOLUME_PUBLIC_URL", "http://localhost:8087"),
		},
	}
}
