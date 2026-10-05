package ports

import (
	"context"
	"errors"

	networkkit "github.com/horizon/orion/libs/go/kit/network"
)

var (
	ErrNetworkRecordNotFound           = errors.New("network not found in store")
	ErrSubnetRecordNotFound            = errors.New("subnet not found in store")
	ErrPortRecordNotFound              = errors.New("port not found in store")
	ErrQuotaExceeded                   = errors.New("project quota exceeded")
	ErrSecurityGroupRecordNotFound     = errors.New("security group not found in store")
	ErrSecurityGroupRuleRecordNotFound = errors.New("security group rule not found in store")
	ErrRouterRecordNotFound            = errors.New("router not found in store")
	ErrRouterInterfaceRecordNotFound   = errors.New("router interface not found in store")
	ErrFloatingIPRecordNotFound        = errors.New("floating ip not found in store")
)

type QuotaStore interface {
	AllocateProjectQuota(ctx context.Context, projectID string, networks, ports int) error
	ReleaseProjectQuota(ctx context.Context, projectID string, networks, ports int) error
}

type FinalizerStore interface {
	EnsureFinalizers(ctx context.Context, resourceType, resourceID string, finalizers []string) error
	RemoveFinalizer(ctx context.Context, resourceType, resourceID, finalizer string) error
	ListFinalizers(ctx context.Context, resourceType, resourceID string) ([]string, error)
}

// SecurityGroupStore persists Neutron-like security groups and rules. It is
// optional so older/testing stores can still provide the core network API.
type SecurityGroupStore interface {
	SaveSecurityGroup(ctx context.Context, group networkkit.SecurityGroup) error
	GetSecurityGroup(ctx context.Context, id string) (networkkit.SecurityGroup, error)
	ListSecurityGroups(ctx context.Context, projectID string) ([]networkkit.SecurityGroup, error)
	DeleteSecurityGroup(ctx context.Context, id string) error
	SaveSecurityGroupRule(ctx context.Context, rule networkkit.SecurityGroupRule) error
	GetSecurityGroupRule(ctx context.Context, id string) (networkkit.SecurityGroupRule, error)
	ListSecurityGroupRules(ctx context.Context, groupID string) ([]networkkit.SecurityGroupRule, error)
	DeleteSecurityGroupRule(ctx context.Context, id string) error
}

type RouterStore interface {
	SaveRouter(ctx context.Context, router networkkit.Router) error
	GetRouter(ctx context.Context, id string) (networkkit.Router, error)
	ListRouters(ctx context.Context, projectID string) ([]networkkit.Router, error)
	DeleteRouter(ctx context.Context, id string) error
	SaveRouterInterface(ctx context.Context, item networkkit.RouterInterface) error
	GetRouterInterface(ctx context.Context, id string) (networkkit.RouterInterface, error)
	ListRouterInterfaces(ctx context.Context, routerID string) ([]networkkit.RouterInterface, error)
	DeleteRouterInterface(ctx context.Context, id string) error
	SaveFloatingIP(ctx context.Context, item networkkit.FloatingIP) error
	GetFloatingIP(ctx context.Context, id string) (networkkit.FloatingIP, error)
	ListFloatingIPs(ctx context.Context, projectID string) ([]networkkit.FloatingIP, error)
	DeleteFloatingIP(ctx context.Context, id string) error
}

// NetworkStore persists network resources.
type NetworkStore interface {
	// Networks
	SaveNetwork(ctx context.Context, n networkkit.Network) error
	GetNetwork(ctx context.Context, id string) (networkkit.Network, error)
	ListNetworks(ctx context.Context) ([]networkkit.Network, error)
	DeleteNetwork(ctx context.Context, id string) error

	// Subnets
	SaveSubnet(ctx context.Context, s networkkit.Subnet) error
	GetSubnet(ctx context.Context, id string) (networkkit.Subnet, error)
	ListSubnets(ctx context.Context) ([]networkkit.Subnet, error)
	FirstSubnetByNetwork(ctx context.Context, networkID string) (networkkit.Subnet, bool)

	// Ports
	SavePort(ctx context.Context, p networkkit.Port) error
	GetPort(ctx context.Context, id string) (networkkit.Port, error)
	ListPorts(ctx context.Context) ([]networkkit.Port, error)
	DeletePort(ctx context.Context, id string) error
	// UsedIPsBySubnet returns the set of IP addresses already assigned in a subnet.
	UsedIPsBySubnet(ctx context.Context, subnetID string) (map[string]struct{}, error)
}
