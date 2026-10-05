package driver

import (
	"fmt"
)

func NewDriver(backend string, cfg Config) (Driver, error) {
	switch backend {
	case "terraform":
		return NewTerraformDriver(cfg)
	case "ansible":
		return NewAnsibleDriver(cfg)
	case "orion":
		return NewOrionDriver(cfg)
	default:
		return nil, fmt.Errorf("unsupported backend: %s", backend)
	}
}
