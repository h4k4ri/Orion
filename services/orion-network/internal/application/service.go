package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/horizon/orion/libs/go/kit/events"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/ids"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/services/orion-network/internal/ports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var (
	ErrBackendUnavailable        = errors.New("network backend unavailable")
	ErrNetworkNotFound           = errors.New("network not found")
	ErrSubnetNotFound            = errors.New("subnet not found")
	ErrPortNotFound              = errors.New("port not found")
	ErrBindingHostMismatch       = errors.New("port binding host mismatch")
	ErrNoSubnetOnNet             = errors.New("no subnet on network")
	ErrInvalidCIDR               = errors.New("invalid cidr")
	ErrSecurityGroupNotFound     = errors.New("security group not found")
	ErrSecurityGroupRuleNotFound = errors.New("security group rule not found")
	ErrInvalidSecurityGroupRule  = errors.New("invalid security group rule")
	ErrQuotaExceeded             = ports.ErrQuotaExceeded
	ErrNetworkInUse              = errors.New("network has dependent resources")
	ErrRouterNotFound            = errors.New("router not found")
	ErrRouterInterfaceNotFound   = errors.New("router interface not found")
	ErrFloatingIPNotFound        = errors.New("floating ip not found")
	ErrRouterBackendUnsupported  = errors.New("router backend is not supported")
	ErrRouterInUse               = errors.New("router has dependent resources")
	ErrInvalidRouterRequest      = errors.New("invalid router request")
)

type Service struct {
	store     ports.NetworkStore
	driver    ports.Driver
	publisher events.Publisher
}

func (s *Service) ListRouters(ctx context.Context, projectID string) ([]networkkit.Router, error) {
	store, ok := s.store.(ports.RouterStore)
	if !ok {
		return nil, ErrRouterBackendUnsupported
	}
	return store.ListRouters(ctx, projectID)
}

func (s *Service) CreateRouter(ctx context.Context, req networkkit.CreateRouterRequest) (networkkit.Router, error) {
	store, storeOK := s.store.(ports.RouterStore)
	driver, driverOK := s.driver.(ports.RouterDriver)
	if !storeOK || !driverOK {
		return networkkit.Router{}, ErrRouterBackendUnsupported
	}
	if req.ProjectID == "" || req.Name == "" {
		return networkkit.Router{}, ErrInvalidRouterRequest
	}
	if req.ExternalNetworkID != "" {
		if _, err := s.store.GetNetwork(ctx, req.ExternalNetworkID); err != nil {
			return networkkit.Router{}, ErrNetworkNotFound
		}
	}
	if req.EnableSNAT && (req.ExternalNetworkID == "" || req.SNATExternalIP == "") {
		return networkkit.Router{}, ErrInvalidRouterRequest
	}
	if req.SNATExternalIP != "" {
		externalSubnet, ok := s.store.FirstSubnetByNetwork(ctx, req.ExternalNetworkID)
		if !ok || !subnetContainsHost(externalSubnet, req.SNATExternalIP) {
			return networkkit.Router{}, ErrInvalidRouterRequest
		}
	}
	now := time.Now().UTC()
	router := networkkit.Router{ID: ids.New("router"), ProjectID: req.ProjectID, Name: req.Name, ExternalNetworkID: req.ExternalNetworkID, EnableSNAT: req.EnableSNAT, SNATExternalIP: req.SNATExternalIP, Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := driver.CreateRouter(router); err != nil {
		return networkkit.Router{}, mapBackendError(err)
	}
	if err := store.SaveRouter(ctx, router); err != nil {
		_ = driver.DeleteRouter(router)
		return networkkit.Router{}, err
	}
	return router, nil
}

func (s *Service) GetRouter(ctx context.Context, id string) (networkkit.Router, error) {
	store, ok := s.store.(ports.RouterStore)
	if !ok {
		return networkkit.Router{}, ErrRouterBackendUnsupported
	}
	router, err := store.GetRouter(ctx, id)
	if errors.Is(err, ports.ErrRouterRecordNotFound) {
		return networkkit.Router{}, ErrRouterNotFound
	}
	return router, err
}

func (s *Service) DeleteRouter(ctx context.Context, id string) error {
	store, storeOK := s.store.(ports.RouterStore)
	driver, driverOK := s.driver.(ports.RouterDriver)
	if !storeOK || !driverOK {
		return ErrRouterBackendUnsupported
	}
	router, err := s.GetRouter(ctx, id)
	if err != nil {
		return err
	}
	interfaces, err := store.ListRouterInterfaces(ctx, id)
	if err != nil {
		return err
	}
	if len(interfaces) > 0 {
		return ErrRouterInUse
	}
	floatingIPs, err := store.ListFloatingIPs(ctx, "")
	if err != nil {
		return err
	}
	for _, item := range floatingIPs {
		if item.RouterID == id {
			return ErrRouterInUse
		}
	}
	if err := driver.DeleteRouter(router); err != nil {
		return mapBackendError(err)
	}
	return store.DeleteRouter(ctx, id)
}

func (s *Service) CreateRouterInterface(ctx context.Context, req networkkit.CreateRouterInterfaceRequest) (networkkit.RouterInterface, error) {
	store, storeOK := s.store.(ports.RouterStore)
	driver, driverOK := s.driver.(ports.RouterDriver)
	if !storeOK || !driverOK {
		return networkkit.RouterInterface{}, ErrRouterBackendUnsupported
	}
	router, err := s.GetRouter(ctx, req.RouterID)
	if err != nil {
		return networkkit.RouterInterface{}, err
	}
	subnet, err := s.GetSubnet(ctx, req.SubnetID)
	if err != nil {
		return networkkit.RouterInterface{}, err
	}
	if subnet.ProjectID != router.ProjectID {
		return networkkit.RouterInterface{}, ErrInvalidRouterRequest
	}
	port, err := s.CreatePort(ctx, networkkit.CreatePortRequest{ProjectID: router.ProjectID, NetworkID: subnet.NetworkID, DeviceID: router.ID, DeviceOwner: "network:router_interface"})
	if err != nil {
		return networkkit.RouterInterface{}, err
	}
	item := networkkit.RouterInterface{ID: ids.New("rtrif"), RouterID: router.ID, SubnetID: subnet.ID, PortID: port.ID, CreatedAt: time.Now().UTC()}
	if err := driver.AddRouterInterface(router, subnet, port); err != nil {
		_ = s.DeletePort(ctx, port.ID)
		return networkkit.RouterInterface{}, mapBackendError(err)
	}
	if err := store.SaveRouterInterface(ctx, item); err != nil {
		_ = driver.RemoveRouterInterface(router, item)
		_ = s.DeletePort(ctx, port.ID)
		return networkkit.RouterInterface{}, err
	}
	return item, nil
}

func (s *Service) ListRouterInterfaces(ctx context.Context, routerID string) ([]networkkit.RouterInterface, error) {
	store, ok := s.store.(ports.RouterStore)
	if !ok {
		return nil, ErrRouterBackendUnsupported
	}
	return store.ListRouterInterfaces(ctx, routerID)
}

func (s *Service) DeleteRouterInterface(ctx context.Context, id string) error {
	store, storeOK := s.store.(ports.RouterStore)
	driver, driverOK := s.driver.(ports.RouterDriver)
	if !storeOK || !driverOK {
		return ErrRouterBackendUnsupported
	}
	item, err := store.GetRouterInterface(ctx, id)
	if errors.Is(err, ports.ErrRouterInterfaceRecordNotFound) {
		return ErrRouterInterfaceNotFound
	}
	if err != nil {
		return err
	}
	router, err := s.GetRouter(ctx, item.RouterID)
	if err != nil {
		return err
	}
	if err := driver.RemoveRouterInterface(router, item); err != nil {
		return mapBackendError(err)
	}
	if err := s.DeletePort(ctx, item.PortID); err != nil && !errors.Is(err, ErrPortNotFound) {
		return err
	}
	return store.DeleteRouterInterface(ctx, id)
}

func (s *Service) ListFloatingIPs(ctx context.Context, projectID string) ([]networkkit.FloatingIP, error) {
	store, ok := s.store.(ports.RouterStore)
	if !ok {
		return nil, ErrRouterBackendUnsupported
	}
	return store.ListFloatingIPs(ctx, projectID)
}

func (s *Service) CreateFloatingIP(ctx context.Context, req networkkit.CreateFloatingIPRequest) (networkkit.FloatingIP, error) {
	store, storeOK := s.store.(ports.RouterStore)
	driver, driverOK := s.driver.(ports.RouterDriver)
	if !storeOK || !driverOK {
		return networkkit.FloatingIP{}, ErrRouterBackendUnsupported
	}
	if req.ProjectID == "" || req.FloatingNetworkID == "" {
		return networkkit.FloatingIP{}, ErrInvalidRouterRequest
	}
	network, err := s.GetNetwork(ctx, req.FloatingNetworkID)
	if err != nil {
		return networkkit.FloatingIP{}, err
	}
	subnet, ok := s.store.FirstSubnetByNetwork(ctx, network.ID)
	if !ok {
		return networkkit.FloatingIP{}, ErrNoSubnetOnNet
	}
	router, err := s.GetRouter(ctx, req.RouterID)
	if err != nil {
		return networkkit.FloatingIP{}, err
	}
	if router.ProjectID != req.ProjectID || router.ExternalNetworkID != network.ID {
		return networkkit.FloatingIP{}, ErrInvalidRouterRequest
	}
	item := networkkit.FloatingIP{ID: ids.New("fip"), ProjectID: req.ProjectID, FloatingNetworkID: network.ID, FloatingIP: req.FloatingIP, RouterID: router.ID, Status: "active", CreatedAt: time.Now().UTC()}
	if item.FloatingIP == "" {
		item.FloatingIP, err = s.nextFixedIP(ctx, subnet)
		if err != nil {
			return networkkit.FloatingIP{}, err
		}
	} else {
		ip := net.ParseIP(item.FloatingIP)
		_, ipnet, parseErr := net.ParseCIDR(subnet.CIDR)
		if ip == nil || parseErr != nil || !ipnet.Contains(ip) || item.FloatingIP == subnet.GatewayIP {
			return networkkit.FloatingIP{}, ErrInvalidRouterRequest
		}
		allocated, listErr := store.ListFloatingIPs(ctx, "")
		if listErr != nil {
			return networkkit.FloatingIP{}, listErr
		}
		for _, existing := range allocated {
			if existing.FloatingNetworkID == network.ID && existing.FloatingIP == item.FloatingIP {
				return networkkit.FloatingIP{}, ErrInvalidRouterRequest
			}
		}
	}
	if req.PortID != "" {
		port, portErr := s.GetPort(ctx, req.PortID)
		if portErr != nil || port.ProjectID != req.ProjectID {
			return networkkit.FloatingIP{}, ErrPortNotFound
		}
		if len(port.FixedIPs) == 0 {
			return networkkit.FloatingIP{}, ErrInvalidRouterRequest
		}
		item.PortID, item.FixedIP = port.ID, port.FixedIPs[0].IPAddress
		if err := driver.CreateFloatingIP(router, item, port); err != nil {
			return networkkit.FloatingIP{}, mapBackendError(err)
		}
	} else if err := driver.CreateFloatingIP(router, item, networkkit.Port{}); err != nil {
		return networkkit.FloatingIP{}, mapBackendError(err)
	}
	if err := store.SaveFloatingIP(ctx, item); err != nil {
		_ = driver.DeleteFloatingIP(router, item)
		return networkkit.FloatingIP{}, err
	}
	return item, nil
}

func (s *Service) GetFloatingIP(ctx context.Context, id string) (networkkit.FloatingIP, error) {
	store, ok := s.store.(ports.RouterStore)
	if !ok {
		return networkkit.FloatingIP{}, ErrRouterBackendUnsupported
	}
	item, err := store.GetFloatingIP(ctx, id)
	if errors.Is(err, ports.ErrFloatingIPRecordNotFound) {
		return networkkit.FloatingIP{}, ErrFloatingIPNotFound
	}
	return item, err
}

func (s *Service) DeleteFloatingIP(ctx context.Context, id string) error {
	store, storeOK := s.store.(ports.RouterStore)
	driver, driverOK := s.driver.(ports.RouterDriver)
	if !storeOK || !driverOK {
		return ErrRouterBackendUnsupported
	}
	item, err := s.GetFloatingIP(ctx, id)
	if err != nil {
		return err
	}
	router, err := s.GetRouter(ctx, item.RouterID)
	if err != nil {
		return err
	}
	if err := driver.DeleteFloatingIP(router, item); err != nil {
		return mapBackendError(err)
	}
	return store.DeleteFloatingIP(ctx, id)
}

func mapBackendError(err error) error {
	if errors.Is(err, ports.ErrBackendUnavailable) {
		return ErrBackendUnavailable
	}
	return err
}

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-network")

func NewService(store ports.NetworkStore, driver ports.Driver, publisher events.Publisher) *Service {
	return &Service{store: store, driver: driver, publisher: publisher}
}

func (s *Service) ListNetworks(ctx context.Context) []networkkit.Network {
	items, _ := s.store.ListNetworks(ctx)
	return items
}

func (s *Service) GetNetwork(ctx context.Context, networkID string) (networkkit.Network, error) {
	n, err := s.store.GetNetwork(ctx, networkID)
	if errors.Is(err, ports.ErrNetworkRecordNotFound) {
		return networkkit.Network{}, ErrNetworkNotFound
	}
	return n, err
}

func (s *Service) DeleteNetwork(ctx context.Context, networkID string) error {
	ctx, span := tracer.Start(ctx, "network.DeleteNetwork")
	defer span.End()

	item, err := s.store.GetNetwork(ctx, networkID)
	if errors.Is(err, ports.ErrNetworkRecordNotFound) {
		return ErrNetworkNotFound
	}
	if err != nil {
		return err
	}
	if _, ok := s.store.FirstSubnetByNetwork(ctx, networkID); ok {
		return ErrNetworkInUse
	}
	for _, port := range s.ListPorts(ctx, "") {
		if port.NetworkID == networkID {
			return ErrNetworkInUse
		}
	}

	finalizerStore, finalizersEnabled := s.store.(ports.FinalizerStore)
	if finalizersEnabled {
		if err := finalizerStore.EnsureFinalizers(ctx, "network", item.ID, []string{"backend.delete", "quota.release"}); err != nil {
			return err
		}
	}
	if err := s.driver.DeleteLogicalSwitch(item); err != nil {
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return ErrBackendUnavailable
		}
		return err
	}
	if finalizersEnabled {
		if err := finalizerStore.RemoveFinalizer(ctx, "network", item.ID, "backend.delete"); err != nil {
			return err
		}
	}
	if quotaStore, ok := s.store.(ports.QuotaStore); ok {
		if err := quotaStore.ReleaseProjectQuota(ctx, item.ProjectID, 1, 0); err != nil {
			return err
		}
	}
	if finalizersEnabled {
		if err := finalizerStore.RemoveFinalizer(ctx, "network", item.ID, "quota.release"); err != nil {
			return err
		}
		remaining, err := finalizerStore.ListFinalizers(ctx, "network", item.ID)
		if err != nil {
			return err
		}
		if len(remaining) > 0 {
			return fmt.Errorf("network %s still has finalizers: %v", item.ID, remaining)
		}
	}
	if err := s.store.DeleteNetwork(ctx, networkID); err != nil {
		return err
	}
	s.publishResource(ctx, "network.deleted", "network", item.ID, item.ProjectID, "deleted")
	return nil
}

func (s *Service) CreateNetwork(ctx context.Context, req networkkit.CreateNetworkRequest) (networkkit.Network, error) {
	ctx, span := tracer.Start(ctx, "network.CreateNetwork")
	defer span.End()

	now := time.Now().UTC()
	quotaStore, quotaEnabled := s.store.(ports.QuotaStore)
	if quotaEnabled {
		if err := quotaStore.AllocateProjectQuota(ctx, req.ProjectID, 1, 0); err != nil {
			return networkkit.Network{}, err
		}
	}
	item := networkkit.Network{
		ID:        ids.New("net"),
		ProjectID: req.ProjectID,
		Name:      req.Name,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.driver.CreateLogicalSwitch(item); err != nil {
		if quotaEnabled {
			_ = quotaStore.ReleaseProjectQuota(ctx, req.ProjectID, 1, 0)
		}
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return networkkit.Network{}, ErrBackendUnavailable
		}
		return networkkit.Network{}, err
	}

	if err := s.store.SaveNetwork(ctx, item); err != nil {
		if quotaEnabled {
			_ = quotaStore.ReleaseProjectQuota(ctx, req.ProjectID, 1, 0)
		}
		return networkkit.Network{}, err
	}
	span.SetAttributes(attribute.String("orion.network_id", item.ID))
	s.publishResource(ctx, "network.created", "network", item.ID, item.ProjectID, item.Status)
	return item, nil
}

func (s *Service) ListSubnets(ctx context.Context) []networkkit.Subnet {
	items, _ := s.store.ListSubnets(ctx)
	return items
}

func (s *Service) GetSubnet(ctx context.Context, subnetID string) (networkkit.Subnet, error) {
	sub, err := s.store.GetSubnet(ctx, subnetID)
	if errors.Is(err, ports.ErrSubnetRecordNotFound) {
		return networkkit.Subnet{}, ErrSubnetNotFound
	}
	return sub, err
}

func (s *Service) CreateSubnet(ctx context.Context, req networkkit.CreateSubnetRequest) (networkkit.Subnet, error) {
	ctx, span := tracer.Start(ctx, "network.CreateSubnet")
	defer span.End()

	network, err := s.store.GetNetwork(ctx, req.NetworkID)
	if errors.Is(err, ports.ErrNetworkRecordNotFound) {
		return networkkit.Subnet{}, ErrNetworkNotFound
	}
	if err != nil {
		return networkkit.Subnet{}, err
	}

	_, ipnet, parseErr := net.ParseCIDR(req.CIDR)
	if parseErr != nil {
		return networkkit.Subnet{}, ErrInvalidCIDR
	}

	gatewayIP := req.GatewayIP
	if gatewayIP == "" {
		gatewayIP, err = firstUsableIP(ipnet)
		if err != nil {
			return networkkit.Subnet{}, ErrInvalidCIDR
		}
	}

	now := time.Now().UTC()
	item := networkkit.Subnet{
		ID:         ids.New("subnet"),
		ProjectID:  req.ProjectID,
		NetworkID:  req.NetworkID,
		Name:       req.Name,
		CIDR:       req.CIDR,
		GatewayIP:  gatewayIP,
		EnableDHCP: req.EnableDHCP,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	dhcpUUID, dhcpErr := s.driver.CreateSubnet(network, item)
	if dhcpErr != nil {
		if errors.Is(dhcpErr, ports.ErrBackendUnavailable) {
			return networkkit.Subnet{}, ErrBackendUnavailable
		}
		return networkkit.Subnet{}, dhcpErr
	}
	item.DHCPOptionsUUID = dhcpUUID

	if err := s.store.SaveSubnet(ctx, item); err != nil {
		return networkkit.Subnet{}, err
	}
	span.SetAttributes(
		attribute.String("orion.subnet_id", item.ID),
		attribute.String("orion.network_id", item.NetworkID),
	)
	s.publishResource(ctx, "subnet.created", "subnet", item.ID, item.ProjectID, "")
	return item, nil
}

func (s *Service) ListPorts(ctx context.Context, bindingHostID string) []networkkit.Port {
	items, _ := s.store.ListPorts(ctx)
	if bindingHostID == "" {
		return items
	}

	filtered := make([]networkkit.Port, 0, len(items))
	for _, item := range items {
		if item.BindingHostID == bindingHostID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *Service) Reconcile(ctx context.Context) (int, error) {
	ctx, span := tracer.Start(ctx, "network.Reconcile")
	defer span.End()

	portsList, err := s.store.ListPorts(ctx)
	if err != nil {
		return 0, err
	}

	reconciled := 0
	for _, item := range portsList {
		refreshed, err := s.refreshPortStatus(ctx, item)
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return reconciled, err
		}
		if err != nil {
			return reconciled, err
		}
		if refreshed.Status != item.Status {
			reconciled++
		}
	}

	span.SetAttributes(attribute.Int("orion.reconciled_ports", reconciled))
	return reconciled, nil
}

func (s *Service) GetPort(ctx context.Context, portID string) (networkkit.Port, error) {
	item, err := s.store.GetPort(ctx, portID)
	if errors.Is(err, ports.ErrPortRecordNotFound) {
		return networkkit.Port{}, ErrPortNotFound
	}
	if err != nil {
		return networkkit.Port{}, err
	}
	refreshed, err := s.refreshPortStatus(ctx, item)
	if err != nil {
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return item, nil
		}
		return networkkit.Port{}, err
	}
	return refreshed, nil
}

func (s *Service) DeletePort(ctx context.Context, portID string) error {
	ctx, span := tracer.Start(ctx, "network.DeletePort")
	defer span.End()

	item, err := s.store.GetPort(ctx, portID)
	if errors.Is(err, ports.ErrPortRecordNotFound) {
		return ErrPortNotFound
	}
	if err != nil {
		return err
	}

	if err := s.driver.DeleteLogicalSwitchPort(portID); err != nil {
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return ErrBackendUnavailable
		}
		return err
	}
	if securityDriver, ok := s.driver.(ports.SecurityGroupDriver); ok {
		if err := securityDriver.DeleteSecurityGroupBindings(portID); err != nil {
			return err
		}
	}

	if err := s.store.DeletePort(ctx, portID); err != nil {
		return err
	}
	if quotaStore, ok := s.store.(ports.QuotaStore); ok {
		_ = quotaStore.ReleaseProjectQuota(ctx, item.ProjectID, 0, 1)
	}
	span.SetAttributes(attribute.String("orion.port_id", portID))
	s.publishResource(ctx, "port.deleted", "port", portID, "", "deleted")
	return nil
}

func (s *Service) UpdatePortBinding(ctx context.Context, portID string, req networkkit.UpdatePortBindingRequest) (networkkit.Port, error) {
	ctx, span := tracer.Start(ctx, "network.UpdatePortBinding")
	defer span.End()

	item, err := s.store.GetPort(ctx, portID)
	if errors.Is(err, ports.ErrPortRecordNotFound) {
		return networkkit.Port{}, ErrPortNotFound
	}
	if err != nil {
		return networkkit.Port{}, err
	}
	if item.BindingHostID != "" && req.BindingHostID != "" && item.BindingHostID != req.BindingHostID {
		return networkkit.Port{}, ErrBindingHostMismatch
	}
	bindingChanged := item.BindingStatus != req.BindingStatus || item.BindingDetail != req.BindingDetail
	if req.BindingHostID != "" && item.BindingHostID != req.BindingHostID {
		bindingChanged = true
	}

	item.BindingStatus = req.BindingStatus
	item.BindingDetail = req.BindingDetail
	item.UpdatedAt = time.Now().UTC()
	if req.BindingHostID != "" {
		item.BindingHostID = req.BindingHostID
	}
	if err := s.store.SavePort(ctx, item); err != nil {
		return networkkit.Port{}, err
	}

	refreshed, refreshErr := s.refreshPortStatus(ctx, item)
	if refreshErr != nil {
		if errors.Is(refreshErr, ports.ErrBackendUnavailable) {
			item.Status = composePortStatus(item, nil)
			if err := s.store.SavePort(ctx, item); err != nil {
				return networkkit.Port{}, err
			}
			refreshed = item
		} else {
			return networkkit.Port{}, refreshErr
		}
	}

	span.SetAttributes(attribute.String("orion.port_id", refreshed.ID))
	if bindingChanged {
		s.publishResource(ctx, "port.binding_updated", "port", refreshed.ID, refreshed.ProjectID, refreshed.Status)
	}
	return refreshed, nil
}

func (s *Service) CreatePort(ctx context.Context, req networkkit.CreatePortRequest) (networkkit.Port, error) {
	ctx, span := tracer.Start(ctx, "network.CreatePort")
	defer span.End()

	network, err := s.store.GetNetwork(ctx, req.NetworkID)
	if errors.Is(err, ports.ErrNetworkRecordNotFound) {
		return networkkit.Port{}, ErrNetworkNotFound
	}
	if err != nil {
		return networkkit.Port{}, err
	}
	if groupStore, ok := s.store.(ports.SecurityGroupStore); ok {
		for _, groupID := range req.SecurityGroupIDs {
			group, groupErr := groupStore.GetSecurityGroup(ctx, groupID)
			if groupErr != nil || group.ProjectID != req.ProjectID {
				return networkkit.Port{}, ErrSecurityGroupNotFound
			}
		}
	}

	subnet, ok := s.store.FirstSubnetByNetwork(ctx, req.NetworkID)
	if !ok {
		return networkkit.Port{}, ErrNoSubnetOnNet
	}
	quotaStore, quotaEnabled := s.store.(ports.QuotaStore)
	quotaCommitted := false
	if quotaEnabled {
		if err := quotaStore.AllocateProjectQuota(ctx, req.ProjectID, 0, 1); err != nil {
			return networkkit.Port{}, err
		}
		defer func() {
			if !quotaCommitted {
				_ = quotaStore.ReleaseProjectQuota(context.Background(), req.ProjectID, 0, 1)
			}
		}()
	}

	ip, err := s.nextFixedIP(ctx, subnet)
	if err != nil {
		return networkkit.Port{}, err
	}

	now := time.Now().UTC()
	item := networkkit.Port{
		ID:            ids.New("port"),
		ProjectID:     req.ProjectID,
		NetworkID:     req.NetworkID,
		DeviceID:      req.DeviceID,
		DeviceOwner:   req.DeviceOwner,
		BindingHostID: req.BindingHostID,
		BindingStatus: initialBindingStatus(req.BindingHostID),
		BindingDetail: initialBindingDetail(req.BindingHostID),
		MACAddress:    randomMAC(),
		FixedIPs: []networkkit.FixedIP{
			{SubnetID: subnet.ID, IPAddress: ip},
		},
		Status:           composePortStatus(networkkit.Port{BindingHostID: req.BindingHostID, BindingStatus: initialBindingStatus(req.BindingHostID)}, nil),
		VIFType:          "ovs",
		VNICType:         "normal",
		SecurityGroupIDs: append([]string(nil), req.SecurityGroupIDs...),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.driver.CreateLogicalSwitchPort(network, subnet, item); err != nil {
		if quotaEnabled {
			_ = quotaStore.ReleaseProjectQuota(ctx, req.ProjectID, 0, 1)
		}
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return networkkit.Port{}, ErrBackendUnavailable
		}
		return networkkit.Port{}, err
	}
	if groupStore, ok := s.store.(ports.SecurityGroupStore); ok {
		if securityDriver, applies := s.driver.(ports.SecurityGroupDriver); applies && len(req.SecurityGroupIDs) > 0 {
			rules := make([]networkkit.SecurityGroupRule, 0)
			for _, groupID := range req.SecurityGroupIDs {
				groupRules, ruleErr := groupStore.ListSecurityGroupRules(ctx, groupID)
				if ruleErr != nil {
					_ = s.driver.DeleteLogicalSwitchPort(item.ID)
					return networkkit.Port{}, ruleErr
				}
				rules = append(rules, groupRules...)
			}
			if applyErr := securityDriver.ApplySecurityGroups(network, item, rules); applyErr != nil {
				_ = s.driver.DeleteLogicalSwitchPort(item.ID)
				return networkkit.Port{}, applyErr
			}
		}
	}

	if err := s.store.SavePort(ctx, item); err != nil {
		_ = s.driver.DeleteLogicalSwitchPort(item.ID)
		if quotaEnabled {
			_ = quotaStore.ReleaseProjectQuota(ctx, req.ProjectID, 0, 1)
		}
		return networkkit.Port{}, err
	}
	quotaCommitted = true

	refreshed, err := s.refreshPortStatus(ctx, item)
	if err != nil {
		if errors.Is(err, ports.ErrBackendUnavailable) {
			refreshed = item
		} else {
			return networkkit.Port{}, err
		}
	}
	span.SetAttributes(
		attribute.String("orion.port_id", refreshed.ID),
		attribute.String("orion.network_id", refreshed.NetworkID),
	)
	s.publishResource(ctx, "port.created", "port", refreshed.ID, refreshed.ProjectID, refreshed.Status)
	return refreshed, nil
}

func (s *Service) ListSecurityGroups(ctx context.Context, projectID string) ([]networkkit.SecurityGroup, error) {
	store, ok := s.store.(ports.SecurityGroupStore)
	if !ok {
		return nil, fmt.Errorf("security groups are not supported by the configured store")
	}
	return store.ListSecurityGroups(ctx, projectID)
}

func (s *Service) CreateSecurityGroup(ctx context.Context, req networkkit.CreateSecurityGroupRequest) (networkkit.SecurityGroup, error) {
	store, ok := s.store.(ports.SecurityGroupStore)
	if !ok {
		return networkkit.SecurityGroup{}, fmt.Errorf("security groups are not supported by the configured store")
	}
	if req.ProjectID == "" || req.Name == "" {
		return networkkit.SecurityGroup{}, fmt.Errorf("project_id and name are required")
	}
	now := time.Now().UTC()
	group := networkkit.SecurityGroup{ID: ids.New("sg"), ProjectID: req.ProjectID, Name: req.Name, Description: req.Description, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSecurityGroup(ctx, group); err != nil {
		return networkkit.SecurityGroup{}, err
	}
	return group, nil
}

func (s *Service) GetSecurityGroup(ctx context.Context, id string) (networkkit.SecurityGroup, error) {
	store, ok := s.store.(ports.SecurityGroupStore)
	if !ok {
		return networkkit.SecurityGroup{}, fmt.Errorf("security groups are not supported by the configured store")
	}
	group, err := store.GetSecurityGroup(ctx, id)
	if errors.Is(err, ports.ErrSecurityGroupRecordNotFound) {
		return networkkit.SecurityGroup{}, ErrSecurityGroupNotFound
	}
	return group, err
}

func (s *Service) DeleteSecurityGroup(ctx context.Context, id string) error {
	store, ok := s.store.(ports.SecurityGroupStore)
	if !ok {
		return fmt.Errorf("security groups are not supported by the configured store")
	}
	if _, err := s.GetSecurityGroup(ctx, id); err != nil {
		return err
	}
	rules, err := store.ListSecurityGroupRules(ctx, id)
	if err != nil {
		return err
	}
	if len(rules) > 0 {
		return fmt.Errorf("security group has rules")
	}
	return store.DeleteSecurityGroup(ctx, id)
}

func (s *Service) ListSecurityGroupRules(ctx context.Context, groupID string) ([]networkkit.SecurityGroupRule, error) {
	store, ok := s.store.(ports.SecurityGroupStore)
	if !ok {
		return nil, fmt.Errorf("security groups are not supported by the configured store")
	}
	if _, err := s.GetSecurityGroup(ctx, groupID); err != nil {
		return nil, err
	}
	return store.ListSecurityGroupRules(ctx, groupID)
}

func (s *Service) CreateSecurityGroupRule(ctx context.Context, req networkkit.CreateSecurityGroupRuleRequest) (networkkit.SecurityGroupRule, error) {
	store, ok := s.store.(ports.SecurityGroupStore)
	if !ok {
		return networkkit.SecurityGroupRule{}, fmt.Errorf("security groups are not supported by the configured store")
	}
	if _, err := s.GetSecurityGroup(ctx, req.SecurityGroupID); err != nil {
		return networkkit.SecurityGroupRule{}, err
	}
	if req.Direction != "ingress" && req.Direction != "egress" {
		return networkkit.SecurityGroupRule{}, ErrInvalidSecurityGroupRule
	}
	if req.EtherType == "" {
		req.EtherType = "IPv4"
	}
	if req.EtherType != "IPv4" && req.EtherType != "IPv6" {
		return networkkit.SecurityGroupRule{}, ErrInvalidSecurityGroupRule
	}
	if req.PortMin < 0 || req.PortMax < 0 || req.PortMin > 65535 || req.PortMax > 65535 || (req.PortMin > 0 && req.PortMax > 0 && req.PortMin > req.PortMax) {
		return networkkit.SecurityGroupRule{}, ErrInvalidSecurityGroupRule
	}
	if req.RemoteCIDR != "" {
		if _, _, err := net.ParseCIDR(req.RemoteCIDR); err != nil {
			return networkkit.SecurityGroupRule{}, ErrInvalidSecurityGroupRule
		}
	}
	now := time.Now().UTC()
	rule := networkkit.SecurityGroupRule{ID: ids.New("sgr"), SecurityGroupID: req.SecurityGroupID, Direction: req.Direction, EtherType: req.EtherType, Protocol: req.Protocol, PortMin: req.PortMin, PortMax: req.PortMax, RemoteCIDR: req.RemoteCIDR, RemoteGroupID: req.RemoteGroupID, Description: req.Description, CreatedAt: now}
	if err := store.SaveSecurityGroupRule(ctx, rule); err != nil {
		return networkkit.SecurityGroupRule{}, err
	}
	return rule, nil
}

func (s *Service) DeleteSecurityGroupRule(ctx context.Context, id string) error {
	store, ok := s.store.(ports.SecurityGroupStore)
	if !ok {
		return fmt.Errorf("security groups are not supported by the configured store")
	}
	if _, err := store.GetSecurityGroupRule(ctx, id); errors.Is(err, ports.ErrSecurityGroupRuleRecordNotFound) {
		return ErrSecurityGroupRuleNotFound
	} else if err != nil {
		return err
	}
	return store.DeleteSecurityGroupRule(ctx, id)
}

func (s *Service) refreshPortStatus(ctx context.Context, port networkkit.Port) (networkkit.Port, error) {
	up, err := s.driver.GetLogicalSwitchPortUp(port.ID)
	if err != nil {
		return networkkit.Port{}, err
	}

	newStatus := composePortStatus(port, &up)

	if port.Status == newStatus {
		return port, nil
	}

	port.Status = newStatus
	port.UpdatedAt = time.Now().UTC()
	_ = s.store.SavePort(ctx, port)
	s.publishResource(ctx, "port.status_changed", "port", port.ID, port.ProjectID, port.Status)
	return port, nil
}

func composePortStatus(port networkkit.Port, ovnUp *bool) string {
	switch port.BindingStatus {
	case "error":
		return "error"
	case "", "ready":
		// continue
	default:
		if port.BindingHostID != "" {
			return "binding"
		}
	}

	if ovnUp == nil {
		if port.BindingHostID != "" && port.BindingStatus == "ready" {
			return "down"
		}
		if port.Status != "" {
			return port.Status
		}
		return "down"
	}
	if *ovnUp {
		return "active"
	}
	return "down"
}

func initialBindingStatus(bindingHostID string) string {
	if bindingHostID == "" {
		return ""
	}
	return "pending"
}

func initialBindingDetail(bindingHostID string) string {
	if bindingHostID == "" {
		return ""
	}
	return "awaiting host realization"
}

func (s *Service) publishResource(ctx context.Context, eventType, resourceType, resourceID, projectID, status string) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.PublishResource(ctx, events.ResourceEvent{
		EventType:    eventType,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ProjectID:    projectID,
		RequestID:    httpx.RequestIDFromContext(ctx),
		Status:       status,
		OccurredAt:   time.Now().UTC(),
	})
}

func (s *Service) nextFixedIP(ctx context.Context, subnet networkkit.Subnet) (string, error) {
	_, ipnet, err := net.ParseCIDR(subnet.CIDR)
	if err != nil {
		return "", ErrInvalidCIDR
	}

	candidates, err := usableIPs(ipnet)
	if err != nil {
		return "", err
	}

	used, err := s.store.UsedIPsBySubnet(ctx, subnet.ID)
	if err != nil {
		return "", err
	}
	used[subnet.GatewayIP] = struct{}{}
	if routerStore, ok := s.store.(ports.RouterStore); ok {
		floatingIPs, listErr := routerStore.ListFloatingIPs(ctx, "")
		if listErr != nil {
			return "", listErr
		}
		for _, item := range floatingIPs {
			if item.FloatingNetworkID == subnet.NetworkID {
				used[item.FloatingIP] = struct{}{}
			}
		}
	}

	for _, candidate := range candidates {
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no available IPs in subnet %s", subnet.ID)
}

func usableIPs(ipnet *net.IPNet) ([]string, error) {
	networkIP := ipnet.IP.To4()
	if networkIP == nil {
		return nil, ErrInvalidCIDR
	}

	maskSize, bits := ipnet.Mask.Size()
	if bits != 32 || maskSize > 30 {
		return nil, ErrInvalidCIDR
	}

	start := ipv4ToUint32(networkIP) + 1
	end := (ipv4ToUint32(networkIP) | ^maskToUint32(ipnet.Mask)) - 1

	items := make([]string, 0, end-start+1)
	for current := start; current <= end; current++ {
		items = append(items, uint32ToIPv4(current).String())
	}
	return items, nil
}

func subnetContainsHost(subnet networkkit.Subnet, address string) bool {
	ip := net.ParseIP(address)
	_, network, err := net.ParseCIDR(subnet.CIDR)
	if ip == nil || err != nil || !network.Contains(ip) {
		return false
	}
	return address != subnet.GatewayIP
}

func firstUsableIP(ipnet *net.IPNet) (string, error) {
	items, err := usableIPs(ipnet)
	if err != nil || len(items) == 0 {
		return "", ErrInvalidCIDR
	}
	return items[0], nil
}

func maskToUint32(mask net.IPMask) uint32 {
	return ipv4ToUint32(net.IP(mask))
}

func ipv4ToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIPv4(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func randomMAC() string {
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err != nil {
		return "fa:16:3e:00:00:01"
	}
	return fmt.Sprintf("fa:16:3e:%02x:%02x:%02x", buf[0], buf[1], buf[2])
}
