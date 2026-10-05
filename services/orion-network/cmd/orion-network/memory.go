package main

import (
	"context"
	"sync"

	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/services/orion-network/internal/ports"
)

type memoryNetworkStore struct {
	mu             sync.RWMutex
	networks       map[string]networkkit.Network
	subnets        map[string]networkkit.Subnet
	portMap        map[string]networkkit.Port
	securityGroups map[string]networkkit.SecurityGroup
	securityRules  map[string]networkkit.SecurityGroupRule
	routers        map[string]networkkit.Router
	routerPorts    map[string]networkkit.RouterInterface
	floatingIPs    map[string]networkkit.FloatingIP
}

func newMemoryNetworkStore() ports.NetworkStore {
	return &memoryNetworkStore{
		networks:       make(map[string]networkkit.Network),
		subnets:        make(map[string]networkkit.Subnet),
		portMap:        make(map[string]networkkit.Port),
		securityGroups: make(map[string]networkkit.SecurityGroup),
		securityRules:  make(map[string]networkkit.SecurityGroupRule),
		routers:        make(map[string]networkkit.Router),
		routerPorts:    make(map[string]networkkit.RouterInterface),
		floatingIPs:    make(map[string]networkkit.FloatingIP),
	}
}

func (m *memoryNetworkStore) SaveNetwork(_ context.Context, n networkkit.Network) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.networks[n.ID] = n
	return nil
}

func (m *memoryNetworkStore) GetNetwork(_ context.Context, id string) (networkkit.Network, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.networks[id]
	if !ok {
		return networkkit.Network{}, ports.ErrNetworkRecordNotFound
	}
	return n, nil
}

func (m *memoryNetworkStore) ListNetworks(_ context.Context) ([]networkkit.Network, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]networkkit.Network, 0, len(m.networks))
	for _, n := range m.networks {
		items = append(items, n)
	}
	return items, nil
}

func (m *memoryNetworkStore) DeleteNetwork(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.networks, id)
	return nil
}

func (m *memoryNetworkStore) SaveSubnet(_ context.Context, s networkkit.Subnet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subnets[s.ID] = s
	return nil
}

func (m *memoryNetworkStore) GetSubnet(_ context.Context, id string) (networkkit.Subnet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.subnets[id]
	if !ok {
		return networkkit.Subnet{}, ports.ErrSubnetRecordNotFound
	}
	return s, nil
}

func (m *memoryNetworkStore) ListSubnets(_ context.Context) ([]networkkit.Subnet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]networkkit.Subnet, 0, len(m.subnets))
	for _, s := range m.subnets {
		items = append(items, s)
	}
	return items, nil
}

func (m *memoryNetworkStore) FirstSubnetByNetwork(_ context.Context, networkID string) (networkkit.Subnet, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.subnets {
		if s.NetworkID == networkID {
			return s, true
		}
	}
	return networkkit.Subnet{}, false
}

func (m *memoryNetworkStore) SavePort(_ context.Context, p networkkit.Port) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.portMap[p.ID] = p
	return nil
}

func (m *memoryNetworkStore) GetPort(_ context.Context, id string) (networkkit.Port, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.portMap[id]
	if !ok {
		return networkkit.Port{}, ports.ErrPortRecordNotFound
	}
	return p, nil
}

func (m *memoryNetworkStore) ListPorts(_ context.Context) ([]networkkit.Port, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]networkkit.Port, 0, len(m.portMap))
	for _, p := range m.portMap {
		items = append(items, p)
	}
	return items, nil
}

func (m *memoryNetworkStore) DeletePort(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.portMap, id)
	return nil
}

func (m *memoryNetworkStore) UsedIPsBySubnet(_ context.Context, subnetID string) (map[string]struct{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	used := make(map[string]struct{})
	for _, p := range m.portMap {
		for _, fip := range p.FixedIPs {
			if fip.SubnetID == subnetID {
				used[fip.IPAddress] = struct{}{}
			}
		}
	}
	return used, nil
}

func (m *memoryNetworkStore) SaveSecurityGroup(_ context.Context, group networkkit.SecurityGroup) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.securityGroups[group.ID] = group
	return nil
}

func (m *memoryNetworkStore) GetSecurityGroup(_ context.Context, id string) (networkkit.SecurityGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	group, ok := m.securityGroups[id]
	if !ok {
		return networkkit.SecurityGroup{}, ports.ErrSecurityGroupRecordNotFound
	}
	return group, nil
}

func (m *memoryNetworkStore) ListSecurityGroups(_ context.Context, projectID string) ([]networkkit.SecurityGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]networkkit.SecurityGroup, 0)
	for _, group := range m.securityGroups {
		if projectID == "" || group.ProjectID == projectID {
			items = append(items, group)
		}
	}
	return items, nil
}

func (m *memoryNetworkStore) DeleteSecurityGroup(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.securityGroups, id)
	return nil
}

func (m *memoryNetworkStore) SaveSecurityGroupRule(_ context.Context, rule networkkit.SecurityGroupRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.securityRules[rule.ID] = rule
	return nil
}

func (m *memoryNetworkStore) GetSecurityGroupRule(_ context.Context, id string) (networkkit.SecurityGroupRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rule, ok := m.securityRules[id]
	if !ok {
		return networkkit.SecurityGroupRule{}, ports.ErrSecurityGroupRuleRecordNotFound
	}
	return rule, nil
}

func (m *memoryNetworkStore) ListSecurityGroupRules(_ context.Context, groupID string) ([]networkkit.SecurityGroupRule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]networkkit.SecurityGroupRule, 0)
	for _, rule := range m.securityRules {
		if groupID == "" || rule.SecurityGroupID == groupID {
			items = append(items, rule)
		}
	}
	return items, nil
}

func (m *memoryNetworkStore) DeleteSecurityGroupRule(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.securityRules, id)
	return nil
}

func (m *memoryNetworkStore) SaveRouter(_ context.Context, router networkkit.Router) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routers[router.ID] = router
	return nil
}
func (m *memoryNetworkStore) GetRouter(_ context.Context, id string) (networkkit.Router, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.routers[id]
	if !ok {
		return networkkit.Router{}, ports.ErrRouterRecordNotFound
	}
	return item, nil
}
func (m *memoryNetworkStore) ListRouters(_ context.Context, projectID string) ([]networkkit.Router, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]networkkit.Router, 0)
	for _, item := range m.routers {
		if projectID == "" || item.ProjectID == projectID {
			items = append(items, item)
		}
	}
	return items, nil
}
func (m *memoryNetworkStore) DeleteRouter(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.routers, id)
	return nil
}
func (m *memoryNetworkStore) SaveRouterInterface(_ context.Context, item networkkit.RouterInterface) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routerPorts[item.ID] = item
	return nil
}
func (m *memoryNetworkStore) GetRouterInterface(_ context.Context, id string) (networkkit.RouterInterface, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.routerPorts[id]
	if !ok {
		return networkkit.RouterInterface{}, ports.ErrRouterInterfaceRecordNotFound
	}
	return item, nil
}
func (m *memoryNetworkStore) ListRouterInterfaces(_ context.Context, routerID string) ([]networkkit.RouterInterface, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]networkkit.RouterInterface, 0)
	for _, item := range m.routerPorts {
		if routerID == "" || item.RouterID == routerID {
			items = append(items, item)
		}
	}
	return items, nil
}
func (m *memoryNetworkStore) DeleteRouterInterface(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.routerPorts, id)
	return nil
}
func (m *memoryNetworkStore) SaveFloatingIP(_ context.Context, item networkkit.FloatingIP) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.floatingIPs[item.ID] = item
	return nil
}
func (m *memoryNetworkStore) GetFloatingIP(_ context.Context, id string) (networkkit.FloatingIP, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.floatingIPs[id]
	if !ok {
		return networkkit.FloatingIP{}, ports.ErrFloatingIPRecordNotFound
	}
	return item, nil
}
func (m *memoryNetworkStore) ListFloatingIPs(_ context.Context, projectID string) ([]networkkit.FloatingIP, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]networkkit.FloatingIP, 0)
	for _, item := range m.floatingIPs {
		if projectID == "" || item.ProjectID == projectID {
			items = append(items, item)
		}
	}
	return items, nil
}
func (m *memoryNetworkStore) DeleteFloatingIP(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.floatingIPs, id)
	return nil
}
