package driver

import (
	"fmt"
)

func NewDriver(backend string, cfg Config) (Driver, error) {
	switch backend {
	case "nats", "jetstream":
		return NewNATSDriver(cfg)
	case "kafka":
		return NewKafkaDriver(cfg)
	default:
		return nil, fmt.Errorf("unsupported backend: %s", backend)
	}
}
