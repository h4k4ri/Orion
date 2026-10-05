package driver

import (
	"fmt"

	"github.com/horizon/orion/plugins/baremetal/bmc"
)

func NewDriver(protocol string, config map[string]interface{}) (bmc.Driver, error) {
	switch protocol {
	case "redfish":
		cfg := RedfishConfig{
			Address:  getString(config, "address"),
			Username: getString(config, "username"),
			Password: getString(config, "password"),
			Insecure: getBool(config, "insecure"),
			Port:     getInt(config, "port"),
		}
		return NewRedfishDriver(cfg)

	case "ipmi":
		cfg := IPMIConfig{
			Address:   getString(config, "address"),
			Username:  getString(config, "username"),
			Password:  getString(config, "password"),
			Interface: getString(config, "interface"),
			Port:      getInt(config, "port"),
		}
		if cfg.Interface == "" {
			cfg.Interface = "lanplus"
		}
		return NewIPMIDriver(cfg)

	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	if v, ok := m[key].(int); ok {
		return v
	}
	return 0
}
