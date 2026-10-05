package driver

import (
	"fmt"
)

func NewDriver(backend string, cfg Config) (Driver, error) {
	switch backend {
	case "containerd":
		return NewContainerdDriver(cfg)
	case "kubernetes", "k8s":
		return NewKubernetesDriver(cfg)
	default:
		return nil, fmt.Errorf("unsupported backend: %s", backend)
	}
}
