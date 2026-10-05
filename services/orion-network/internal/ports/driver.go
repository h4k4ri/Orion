package ports

import "errors"

import networkkit "github.com/horizon/orion/libs/go/kit/network"

var ErrBackendUnavailable = errors.New("network backend unavailable")

type Driver interface {
	CreateLogicalSwitch(network networkkit.Network) error
	DeleteLogicalSwitch(network networkkit.Network) error
	CreateSubnet(network networkkit.Network, subnet networkkit.Subnet) (string, error)
	CreateLogicalSwitchPort(network networkkit.Network, subnet networkkit.Subnet, port networkkit.Port) error
	DeleteLogicalSwitchPort(portID string) error
	GetLogicalSwitchPortUp(portID string) (bool, error)
}

// SecurityGroupDriver applies the control-plane rules to the provider. It is
// optional so providers without ACL support can still manage ports.
type SecurityGroupDriver interface {
	ApplySecurityGroups(network networkkit.Network, port networkkit.Port, rules []networkkit.SecurityGroupRule) error
	DeleteSecurityGroupBindings(portID string) error
}

type RouterDriver interface {
	CreateRouter(router networkkit.Router) error
	DeleteRouter(router networkkit.Router) error
	AddRouterInterface(router networkkit.Router, subnet networkkit.Subnet, port networkkit.Port) error
	RemoveRouterInterface(router networkkit.Router, item networkkit.RouterInterface) error
	CreateFloatingIP(router networkkit.Router, item networkkit.FloatingIP, port networkkit.Port) error
	DeleteFloatingIP(router networkkit.Router, item networkkit.FloatingIP) error
}
