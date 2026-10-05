package application

import (
	"context"
	"testing"

	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/services/orion-network/internal/ports"
)

type networkStoreStub struct {
	ports map[string]networkkit.Port
}

func newNetworkStoreStub(items ...networkkit.Port) *networkStoreStub {
	store := &networkStoreStub{ports: map[string]networkkit.Port{}}
	for _, item := range items {
		store.ports[item.ID] = item
	}
	return store
}

func (s *networkStoreStub) SaveNetwork(context.Context, networkkit.Network) error {
	return nil
}

func (s *networkStoreStub) GetNetwork(context.Context, string) (networkkit.Network, error) {
	return networkkit.Network{}, ports.ErrNetworkRecordNotFound
}

func (s *networkStoreStub) ListNetworks(context.Context) ([]networkkit.Network, error) {
	return nil, nil
}

func (s *networkStoreStub) DeleteNetwork(context.Context, string) error {
	return nil
}

func (s *networkStoreStub) SaveSubnet(context.Context, networkkit.Subnet) error {
	return nil
}

func (s *networkStoreStub) GetSubnet(context.Context, string) (networkkit.Subnet, error) {
	return networkkit.Subnet{}, ports.ErrSubnetRecordNotFound
}

func (s *networkStoreStub) ListSubnets(context.Context) ([]networkkit.Subnet, error) {
	return nil, nil
}

func (s *networkStoreStub) FirstSubnetByNetwork(context.Context, string) (networkkit.Subnet, bool) {
	return networkkit.Subnet{}, false
}

func (s *networkStoreStub) SavePort(_ context.Context, port networkkit.Port) error {
	s.ports[port.ID] = port
	return nil
}

func (s *networkStoreStub) GetPort(_ context.Context, id string) (networkkit.Port, error) {
	item, ok := s.ports[id]
	if !ok {
		return networkkit.Port{}, ports.ErrPortRecordNotFound
	}
	return item, nil
}

func (s *networkStoreStub) ListPorts(context.Context) ([]networkkit.Port, error) {
	items := make([]networkkit.Port, 0, len(s.ports))
	for _, item := range s.ports {
		items = append(items, item)
	}
	return items, nil
}

func (s *networkStoreStub) DeletePort(_ context.Context, id string) error {
	delete(s.ports, id)
	return nil
}

func (s *networkStoreStub) UsedIPsBySubnet(context.Context, string) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

type networkDriverStub struct {
	portUp bool
	err    error
}

func (s networkDriverStub) CreateLogicalSwitch(networkkit.Network) error {
	return nil
}

func (s networkDriverStub) DeleteLogicalSwitch(networkkit.Network) error {
	return nil
}

func (s networkDriverStub) CreateSubnet(networkkit.Network, networkkit.Subnet) (string, error) {
	return "", nil
}

func (s networkDriverStub) CreateLogicalSwitchPort(networkkit.Network, networkkit.Subnet, networkkit.Port) error {
	return nil
}

func (s networkDriverStub) DeleteLogicalSwitchPort(string) error {
	return nil
}

func (s networkDriverStub) GetLogicalSwitchPortUp(string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.portUp, nil
}

func TestListPortsFiltersByBindingHostID(t *testing.T) {
	service := NewService(
		newNetworkStoreStub(
			networkkit.Port{ID: "port_a", BindingHostID: "host-a"},
			networkkit.Port{ID: "port_b", BindingHostID: "host-b"},
			networkkit.Port{ID: "port_c", BindingHostID: "host-a"},
		),
		nil,
		nil,
	)

	items := service.ListPorts(context.Background(), "host-a")
	if len(items) != 2 {
		t.Fatalf("expected 2 ports for host-a, got %d", len(items))
	}
}

func TestUpdatePortBindingMovesPortToBindingBeforeOvnUp(t *testing.T) {
	store := newNetworkStoreStub(networkkit.Port{
		ID:            "port_a",
		BindingHostID: "host-a",
		BindingStatus: "pending",
		Status:        "binding",
	})
	service := NewService(store, networkDriverStub{portUp: false}, nil)

	item, err := service.UpdatePortBinding(context.Background(), "port_a", networkkit.UpdatePortBindingRequest{
		BindingHostID: "host-a",
		BindingStatus: "ready",
	})
	if err != nil {
		t.Fatalf("update binding failed: %v", err)
	}
	if item.Status != "down" {
		t.Fatalf("expected down status while OVN port is not up, got %q", item.Status)
	}
	if item.BindingStatus != "ready" {
		t.Fatalf("expected ready binding status, got %q", item.BindingStatus)
	}
}

func TestUpdatePortBindingMarksPortError(t *testing.T) {
	store := newNetworkStoreStub(networkkit.Port{
		ID:            "port_a",
		BindingHostID: "host-a",
		BindingStatus: "pending",
		Status:        "binding",
	})
	service := NewService(store, networkDriverStub{portUp: true}, nil)

	item, err := service.UpdatePortBinding(context.Background(), "port_a", networkkit.UpdatePortBindingRequest{
		BindingHostID: "host-a",
		BindingStatus: "error",
		BindingDetail: "failed to create br-int",
	})
	if err != nil {
		t.Fatalf("update binding failed: %v", err)
	}
	if item.Status != "error" {
		t.Fatalf("expected error status, got %q", item.Status)
	}
	if item.BindingDetail != "failed to create br-int" {
		t.Fatalf("expected binding detail to be persisted, got %q", item.BindingDetail)
	}
}
