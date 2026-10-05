package ports

import (
	"context"

	"github.com/horizon/orion/libs/go/kit/authn"
)

type TokenValidator interface {
	Validate(ctx context.Context, token string) (authn.Token, error)
}
