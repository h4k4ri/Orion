package domain

import (
	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
)

type CreateServerCommand struct {
	Actor   authn.Actor                 `json:"actor"`
	Request compute.CreateServerRequest `json:"request"`
}

type AttachVolumeCommand struct {
	Actor   authn.Actor                 `json:"actor"`
	Request compute.AttachVolumeRequest `json:"request"`
}

type ExecutorRequest struct {
	Server compute.Server `json:"server"`
}

type ExecutionResult struct {
	Status string `json:"status"`
}
