package ovn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/services/orion-network/internal/ports"
	libovsdbclient "github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"github.com/ovn-org/libovsdb/ovsdb"
)

const (
	nbDBName         = "OVN_Northbound"
	defaultNBAddress = "unix:/var/run/ovn/ovnnb_db.sock"
	requestTimeout   = 5 * time.Second
)

type Driver struct {
	nbDB string

	mu     sync.Mutex
	client libovsdbclient.Client
}

func New(nbDB string) *Driver {
	return &Driver{
		nbDB: strings.TrimSpace(nbDB),
	}
}

func (d *Driver) CreateLogicalSwitch(network networkkit.Network) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	existing := &logicalSwitch{Name: network.ID}
	if err := ovs.Get(ctx, existing); err == nil {
		existing.ExternalIDs = networkExternalIDs(network)
		ops, err := ovs.Where(existing).Update(existing, &existing.ExternalIDs)
		if err != nil {
			return err
		}
		return transact(ctx, ovs, ops...)
	} else if !errors.Is(err, libovsdbclient.ErrNotFound) {
		return d.wrap(err)
	}

	row := &logicalSwitch{
		UUID:        namedUUID("ls", network.ID),
		Name:        network.ID,
		ExternalIDs: networkExternalIDs(network),
	}
	ops, err := ovs.Create(row)
	if err != nil {
		return d.wrap(err)
	}
	return transact(ctx, ovs, ops...)
}

func (d *Driver) CreateRouter(router networkkit.Router) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	existing := &logicalRouter{Name: router.ID}
	if err := ovs.Get(ctx, existing); err == nil {
		existing.ExternalIDs = routerExternalIDs(router)
		ops, err := ovs.Where(existing).Update(existing, &existing.ExternalIDs)
		if err != nil {
			return err
		}
		if err := transact(ctx, ovs, ops...); err != nil {
			return err
		}
		return d.ensureRouterSNAT(ctx, ovs, router)
	} else if !errors.Is(err, libovsdbclient.ErrNotFound) {
		return d.wrap(err)
	}
	row := &logicalRouter{UUID: namedUUID("lr", router.ID), Name: router.ID, ExternalIDs: routerExternalIDs(router)}
	ops, err := ovs.Create(row)
	if err != nil {
		return d.wrap(err)
	}
	if err := transact(ctx, ovs, ops...); err != nil {
		return err
	}
	return d.ensureRouterSNAT(ctx, ovs, router)
}

func (d *Driver) DeleteRouter(router networkkit.Router) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	row := &logicalRouter{Name: router.ID}
	if err := ovs.Get(ctx, row); err != nil {
		if errors.Is(err, libovsdbclient.ErrNotFound) {
			return nil
		}
		return d.wrap(err)
	}
	var natRows []nat
	if err := ovs.WhereCache(func(item *nat) bool { return item.ExternalIDs["orion_router_id"] == router.ID }).List(ctx, &natRows); err != nil {
		return d.wrap(err)
	}
	var ops []ovsdb.Operation
	for index := range natRows {
		deleteOps, deleteErr := ovs.Where(&natRows[index]).Delete()
		if deleteErr != nil {
			return d.wrap(deleteErr)
		}
		ops = append(ops, deleteOps...)
	}
	deleteRouter, err := ovs.Where(row).Delete()
	if err != nil {
		return d.wrap(err)
	}
	ops = append(ops, deleteRouter...)
	return transact(ctx, ovs, ops...)
}

func (d *Driver) ensureRouterSNAT(ctx context.Context, ovs libovsdbclient.Client, router networkkit.Router) error {
	if !router.EnableSNAT || router.SNATExternalIP == "" {
		return nil
	}
	lr := &logicalRouter{Name: router.ID}
	if err := ovs.Get(ctx, lr); err != nil {
		return d.wrap(err)
	}
	var existing []nat
	if err := ovs.WhereCache(func(item *nat) bool { return item.ExternalIDs["orion_router_id"] == router.ID && item.Type == "snat" }).List(ctx, &existing); err != nil {
		return d.wrap(err)
	}
	if len(existing) > 0 {
		return nil
	}
	row := &nat{UUID: namedUUID("snat", router.ID), Type: "snat", LogicalIP: "0.0.0.0/0", ExternalIP: router.SNATExternalIP, ExternalIDs: map[string]string{"orion_router_id": router.ID, "orion_nat_type": "snat"}}
	createOps, err := ovs.Create(row)
	if err != nil {
		return d.wrap(err)
	}
	mutate, err := ovs.Where(lr).Mutate(lr, model.Mutation{Field: &lr.NAT, Mutator: ovsdb.MutateOperationInsert, Value: []string{row.UUID}})
	if err != nil {
		return d.wrap(err)
	}
	return transact(ctx, ovs, append(createOps, mutate...)...)
}

func (d *Driver) AddRouterInterface(router networkkit.Router, subnet networkkit.Subnet, port networkkit.Port) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	lr := &logicalRouter{Name: router.ID}
	if err := ovs.Get(ctx, lr); err != nil {
		return d.wrap(err)
	}
	if len(port.FixedIPs) == 0 {
		return fmt.Errorf("router interface port has no fixed IP")
	}
	lrpName := routerPortName(router.ID, port.ID)
	lrp := &logicalRouterPort{UUID: namedUUID("lrp", port.ID), Name: lrpName, MAC: port.MACAddress, Networks: []string{port.FixedIPs[0].IPAddress + "/" + cidrPrefix(subnet.CIDR)}, ExternalIDs: map[string]string{"orion_router_id": router.ID, "orion_port_id": port.ID}}
	if err := ovs.Get(ctx, &logicalRouterPort{Name: lrpName}); err == nil {
		return nil
	} else if !errors.Is(err, libovsdbclient.ErrNotFound) {
		return d.wrap(err)
	}
	lsp := &logicalSwitchPort{Name: ovnPortName(port.ID)}
	if err := ovs.Get(ctx, lsp); err != nil {
		return d.wrap(err)
	}
	lsp.Type = "router"
	lsp.Options = map[string]string{"router-port": lrpName}
	lsp.Addresses = []string{"router"}
	createOps, err := ovs.Create(lrp)
	if err != nil {
		return d.wrap(err)
	}
	updateLSP, err := ovs.Where(lsp).Update(lsp, &lsp.Type, &lsp.Options, &lsp.Addresses)
	if err != nil {
		return d.wrap(err)
	}
	mutateRouter, err := ovs.Where(lr).Mutate(lr, model.Mutation{Field: &lr.Ports, Mutator: ovsdb.MutateOperationInsert, Value: []string{lrp.UUID}})
	if err != nil {
		return d.wrap(err)
	}
	return transact(ctx, ovs, append(createOps, append(updateLSP, mutateRouter...)...)...)
}

func (d *Driver) RemoveRouterInterface(router networkkit.Router, item networkkit.RouterInterface) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	lr := &logicalRouter{Name: router.ID}
	if err := ovs.Get(ctx, lr); err != nil {
		return d.wrap(err)
	}
	port := &logicalSwitchPort{Name: ovnPortName(item.PortID)}
	var ops []ovsdb.Operation
	if err := ovs.Get(ctx, port); err == nil {
		port.Type = ""
		port.Options = nil
		port.Addresses = nil
		update, updateErr := ovs.Where(port).Update(port, &port.Type, &port.Options, &port.Addresses)
		if updateErr != nil {
			return d.wrap(updateErr)
		}
		ops = append(ops, update...)
	} else if !errors.Is(err, libovsdbclient.ErrNotFound) {
		return d.wrap(err)
	}
	lrp := &logicalRouterPort{Name: routerPortName(router.ID, item.PortID)}
	if err := ovs.Get(ctx, lrp); err == nil {
		deleteLRP, deleteErr := ovs.Where(lrp).Delete()
		if deleteErr != nil {
			return d.wrap(deleteErr)
		}
		ops = append(ops, deleteLRP...)
		mutate, mutateErr := ovs.Where(lr).Mutate(lr, model.Mutation{Field: &lr.Ports, Mutator: ovsdb.MutateOperationDelete, Value: []string{lrp.UUID}})
		if mutateErr != nil {
			return d.wrap(mutateErr)
		}
		ops = append(ops, mutate...)
	} else if !errors.Is(err, libovsdbclient.ErrNotFound) {
		return d.wrap(err)
	}
	return transact(ctx, ovs, ops...)
}

func (d *Driver) CreateFloatingIP(router networkkit.Router, item networkkit.FloatingIP, port networkkit.Port) error {
	if port.ID == "" || item.FixedIP == "" {
		return nil
	}
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	lr := &logicalRouter{Name: router.ID}
	if err := ovs.Get(ctx, lr); err != nil {
		return d.wrap(err)
	}
	logicalPort := ovnPortName(port.ID)
	row := &nat{UUID: namedUUID("nat", item.ID), Type: "dnat_and_snat", LogicalIP: item.FixedIP, ExternalIP: item.FloatingIP, LogicalPort: &logicalPort, ExternalIDs: map[string]string{"orion_floating_ip_id": item.ID}}
	ops, err := ovs.Create(row)
	if err != nil {
		return d.wrap(err)
	}
	mutate, err := ovs.Where(lr).Mutate(lr, model.Mutation{Field: &lr.NAT, Mutator: ovsdb.MutateOperationInsert, Value: []string{row.UUID}})
	if err != nil {
		return d.wrap(err)
	}
	return transact(ctx, ovs, append(ops, mutate...)...)
}

func (d *Driver) DeleteFloatingIP(router networkkit.Router, item networkkit.FloatingIP) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	lr := &logicalRouter{Name: router.ID}
	if err := ovs.Get(ctx, lr); err != nil {
		return d.wrap(err)
	}
	row := &nat{UUID: namedUUID("nat", item.ID)}
	var ops []ovsdb.Operation
	if err := ovs.Get(ctx, row); err == nil {
		deleteOps, deleteErr := ovs.Where(row).Delete()
		if deleteErr != nil {
			return d.wrap(deleteErr)
		}
		ops = append(ops, deleteOps...)
		mutate, mutateErr := ovs.Where(lr).Mutate(lr, model.Mutation{Field: &lr.NAT, Mutator: ovsdb.MutateOperationDelete, Value: []string{row.UUID}})
		if mutateErr != nil {
			return d.wrap(mutateErr)
		}
		ops = append(ops, mutate...)
	} else if !errors.Is(err, libovsdbclient.ErrNotFound) {
		return d.wrap(err)
	}
	return transact(ctx, ovs, ops...)
}

func (d *Driver) DeleteLogicalSwitch(network networkkit.Network) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	ls := &logicalSwitch{Name: network.ID}
	if err := ovs.Get(ctx, ls); err != nil {
		if errors.Is(err, libovsdbclient.ErrNotFound) {
			return nil
		}
		return d.wrap(err)
	}
	ops, err := ovs.Where(ls).Delete()
	if err != nil {
		return d.wrap(err)
	}
	return transact(ctx, ovs, ops...)
}

func (d *Driver) CreateSubnet(_ networkkit.Network, subnet networkkit.Subnet) (string, error) {
	ovs, err := d.ensureClient()
	if err != nil {
		return "", err
	}

	row := &dhcpOptions{
		UUID:        namedUUID("dhcp", subnet.ID),
		CIDR:        subnet.CIDR,
		Options:     subnetOptions(subnet),
		ExternalIDs: subnetExternalIDs(subnet),
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	ops, err := ovs.Create(row)
	if err != nil {
		return "", d.wrap(err)
	}
	if err := transact(ctx, ovs, ops...); err != nil {
		return "", err
	}

	created := &dhcpOptions{UUID: row.UUID}
	if err := ovs.Get(ctx, created); err != nil {
		return "", d.wrap(err)
	}
	return created.UUID, nil
}

func (d *Driver) CreateLogicalSwitchPort(network networkkit.Network, subnet networkkit.Subnet, port networkkit.Port) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	ls := &logicalSwitch{Name: network.ID}
	if err := ovs.Get(ctx, ls); err != nil {
		return d.wrap(err)
	}

	portName := ovnPortName(port.ID)
	addresses := []string{port.MACAddress}
	if len(port.FixedIPs) > 0 {
		addresses = []string{port.MACAddress + " " + port.FixedIPs[0].IPAddress}
	}

	externalIDs := portExternalIDs(port)
	var dhcpRef *string
	if subnet.EnableDHCP && subnet.DHCPOptionsUUID != "" {
		dhcpRef = &subnet.DHCPOptionsUUID
	}

	lsp := &logicalSwitchPort{Name: portName}
	if err := ovs.Get(ctx, lsp); err == nil {
		lsp.Addresses = addresses
		lsp.PortSecurity = addresses
		lsp.DHCPv4Options = dhcpRef
		lsp.ExternalIDs = externalIDs
		ops, err := ovs.Where(lsp).Update(lsp, &lsp.Addresses, &lsp.PortSecurity, &lsp.DHCPv4Options, &lsp.ExternalIDs)
		if err != nil {
			return d.wrap(err)
		}
		return transact(ctx, ovs, ops...)
	} else if !errors.Is(err, libovsdbclient.ErrNotFound) {
		return d.wrap(err)
	}

	lsp = &logicalSwitchPort{
		UUID:          namedUUID("lsp", port.ID),
		Name:          portName,
		Addresses:     addresses,
		PortSecurity:  addresses,
		DHCPv4Options: dhcpRef,
		ExternalIDs:   externalIDs,
	}

	createOps, err := ovs.Create(lsp)
	if err != nil {
		return d.wrap(err)
	}
	mutateOps, err := ovs.Where(ls).Mutate(ls, model.Mutation{
		Field:   &ls.Ports,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{lsp.UUID},
	})
	if err != nil {
		return d.wrap(err)
	}
	return transact(ctx, ovs, append(createOps, mutateOps...)...)
}

// ApplySecurityGroups translates the Orion security-group contract into OVN
// ACLs attached to the logical switch. Ingress is deny-by-default and each
// declared rule gets a higher-priority allow ACL.
func (d *Driver) ApplySecurityGroups(network networkkit.Network, port networkkit.Port, rules []networkkit.SecurityGroupRule) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	ls := &logicalSwitch{Name: network.ID}
	if err := ovs.Get(ctx, ls); err != nil {
		return d.wrap(err)
	}
	ops, err := d.deletePortACLs(ctx, ovs, ls, port.ID)
	if err != nil {
		return err
	}

	defaultACL := &acl{
		UUID:        namedUUID("acl", port.ID+"-default-ingress"),
		Direction:   "to-lport",
		Priority:    1000,
		Match:       fmt.Sprintf(`inport == "%s"`, ovnPortName(port.ID)),
		Action:      "drop",
		ExternalIDs: map[string]string{"orion_port_id": port.ID, "orion_security_group": "default"},
	}
	createOps, err := ovs.Create(defaultACL)
	if err != nil {
		return d.wrap(err)
	}
	ops = append(ops, createOps...)
	ls.ACLs = append(ls.ACLs, defaultACL.UUID)

	for index, rule := range rules {
		allow := &acl{
			UUID:        namedUUID("acl", fmt.Sprintf("%s-rule-%d", port.ID, index)),
			Direction:   aclDirection(rule.Direction),
			Priority:    2000,
			Match:       securityRuleMatch(port.ID, rule),
			Action:      "allow-related",
			ExternalIDs: map[string]string{"orion_port_id": port.ID, "orion_security_group_rule": rule.ID},
		}
		createOps, err := ovs.Create(allow)
		if err != nil {
			return d.wrap(err)
		}
		ops = append(ops, createOps...)
		ls.ACLs = append(ls.ACLs, allow.UUID)
	}

	updateOps, err := ovs.Where(ls).Update(ls, &ls.ACLs)
	if err != nil {
		return d.wrap(err)
	}
	ops = append(ops, updateOps...)
	return transact(ctx, ovs, ops...)
}

func (d *Driver) DeleteSecurityGroupBindings(portID string) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	var switches []logicalSwitch
	if err := ovs.List(ctx, &switches); err != nil {
		return d.wrap(err)
	}
	var ops []ovsdb.Operation
	for index := range switches {
		ls := &switches[index]
		deleteOps, err := d.deletePortACLs(ctx, ovs, ls, portID)
		if err != nil {
			return err
		}
		ops = append(ops, deleteOps...)
	}
	return transact(ctx, ovs, ops...)
}

func (d *Driver) deletePortACLs(ctx context.Context, ovs libovsdbclient.Client, ls *logicalSwitch, portID string) ([]ovsdb.Operation, error) {
	var existing []acl
	if err := ovs.WhereCache(func(row *acl) bool {
		return row.ExternalIDs["orion_port_id"] == portID
	}).List(ctx, &existing); err != nil {
		return nil, d.wrap(err)
	}
	aclIDs := make(map[string]struct{}, len(existing))
	var ops []ovsdb.Operation
	for index := range existing {
		row := &existing[index]
		aclIDs[row.UUID] = struct{}{}
		deleteOps, err := ovs.Where(row).Delete()
		if err != nil {
			return nil, d.wrap(err)
		}
		ops = append(ops, deleteOps...)
	}
	if len(aclIDs) == 0 {
		return ops, nil
	}
	filtered := make([]string, 0, len(ls.ACLs))
	for _, id := range ls.ACLs {
		if _, remove := aclIDs[id]; !remove {
			filtered = append(filtered, id)
		}
	}
	ls.ACLs = filtered
	updateOps, err := ovs.Where(ls).Update(ls, &ls.ACLs)
	if err != nil {
		return nil, d.wrap(err)
	}
	return append(ops, updateOps...), nil
}

func aclDirection(direction string) string {
	if direction == "egress" {
		return "from-lport"
	}
	return "to-lport"
}

func securityRuleMatch(portID string, rule networkkit.SecurityGroupRule) string {
	portName := ovnPortName(portID)
	match := fmt.Sprintf(`%s == "%s"`, map[string]string{"ingress": "inport", "egress": "outport"}[rule.Direction], portName)
	if rule.EtherType == "IPv6" {
		match += " && ip6"
	} else {
		match += " && ip4"
	}
	if rule.RemoteCIDR != "" {
		field := "ip4.src"
		if rule.EtherType == "IPv6" {
			field = "ip6.src"
		}
		if rule.Direction == "egress" {
			field = strings.Replace(field, ".src", ".dst", 1)
		}
		match += fmt.Sprintf(" && %s == %s", field, rule.RemoteCIDR)
	}
	if rule.Protocol != "" {
		match += " && " + strings.ToLower(rule.Protocol)
		if rule.PortMin > 0 {
			field := strings.ToLower(rule.Protocol) + ".dst"
			if rule.PortMax > rule.PortMin {
				match += fmt.Sprintf(" && %s >= %d && %s <= %d", field, rule.PortMin, field, rule.PortMax)
			} else {
				match += fmt.Sprintf(" && %s == %d", field, rule.PortMin)
			}
		}
	}
	return match
}

func (d *Driver) DeleteLogicalSwitchPort(portID string) error {
	ovs, err := d.ensureClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	lsp := &logicalSwitchPort{Name: ovnPortName(portID)}
	if err := ovs.Get(ctx, lsp); err != nil {
		if errors.Is(err, libovsdbclient.ErrNotFound) {
			return nil
		}
		return d.wrap(err)
	}

	var switches []logicalSwitch
	if err := ovs.WhereCache(func(ls *logicalSwitch) bool {
		for _, portUUID := range ls.Ports {
			if portUUID == lsp.UUID {
				return true
			}
		}
		return false
	}).List(ctx, &switches); err != nil {
		return d.wrap(err)
	}

	var ops []ovsdb.Operation
	for i := range switches {
		ls := switches[i]
		mutateOps, err := ovs.Where(&ls).Mutate(&ls, model.Mutation{
			Field:   &ls.Ports,
			Mutator: ovsdb.MutateOperationDelete,
			Value:   []string{lsp.UUID},
		})
		if err != nil {
			return d.wrap(err)
		}
		ops = append(ops, mutateOps...)
	}

	deleteOps, err := ovs.Where(lsp).Delete()
	if err != nil {
		return d.wrap(err)
	}
	ops = append(ops, deleteOps...)
	return transact(ctx, ovs, ops...)
}

func (d *Driver) GetLogicalSwitchPortUp(portID string) (bool, error) {
	ovs, err := d.ensureClient()
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	lsp := &logicalSwitchPort{Name: ovnPortName(portID)}
	if err := ovs.Get(ctx, lsp); err != nil {
		return false, d.wrap(err)
	}
	return lsp.Up != nil && *lsp.Up, nil
}

func (d *Driver) ensureClient() (libovsdbclient.Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.client != nil && d.client.Connected() {
		return d.client, nil
	}
	if d.client != nil {
		d.client.Disconnect()
		d.client.Close()
		d.client = nil
	}

	clientDBModel, err := model.NewClientDBModel(nbDBName, map[string]model.Model{
		"Logical_Switch":      &logicalSwitch{},
		"DHCP_Options":        &dhcpOptions{},
		"Logical_Switch_Port": &logicalSwitchPort{},
		"ACL":                 &acl{},
		"Logical_Router":      &logicalRouter{},
		"Logical_Router_Port": &logicalRouterPort{},
		"NAT":                 &nat{},
	})
	if err != nil {
		return nil, d.wrap(err)
	}

	ovs, err := libovsdbclient.NewOVSDBClient(clientDBModel, libovsdbclient.WithEndpoint(d.endpoint()))
	if err != nil {
		return nil, d.wrap(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	if err := ovs.Connect(ctx); err != nil {
		ovs.Close()
		return nil, d.wrap(err)
	}
	if _, err := ovs.MonitorAll(ctx); err != nil {
		ovs.Disconnect()
		ovs.Close()
		return nil, d.wrap(err)
	}

	d.client = ovs
	return d.client, nil
}

func (d *Driver) endpoint() string {
	if d.nbDB != "" {
		return d.nbDB
	}
	return defaultNBAddress
}

func (d *Driver) wrap(err error) error {
	if err == nil {
		return nil
	}
	if isBackendUnavailable(err) {
		return fmt.Errorf("%w: ovn-nb", ports.ErrBackendUnavailable)
	}
	return err
}

func transact(ctx context.Context, ovs libovsdbclient.Client, ops ...ovsdb.Operation) error {
	if len(ops) == 0 {
		return nil
	}
	reply, err := ovs.Transact(ctx, ops...)
	if err != nil {
		return err
	}
	_, err = ovsdb.CheckOperationResults(reply, ops)
	return err
}

func isBackendUnavailable(err error) bool {
	message := err.Error()
	return strings.Contains(message, "connection refused") ||
		strings.Contains(message, "no such file or directory") ||
		strings.Contains(message, "permission denied") ||
		strings.Contains(message, "not connected") ||
		strings.Contains(message, "dial unix")
}

func networkExternalIDs(network networkkit.Network) map[string]string {
	return map[string]string{
		"orion-network-id": network.ID,
		"orion-project-id": network.ProjectID,
		"orion-name":       network.Name,
	}
}

func routerExternalIDs(router networkkit.Router) map[string]string {
	return map[string]string{"orion-router-id": router.ID, "orion-project-id": router.ProjectID, "orion-external-network-id": router.ExternalNetworkID, "orion-snat-external-ip": router.SNATExternalIP}
}
func routerPortName(routerID, portID string) string { return "orion-lrp-" + routerID + "-" + portID }
func cidrPrefix(cidr string) string {
	if _, network, err := net.ParseCIDR(cidr); err == nil {
		ones, _ := network.Mask.Size()
		return strconv.Itoa(ones)
	}
	return "32"
}

func subnetExternalIDs(subnet networkkit.Subnet) map[string]string {
	return map[string]string{
		"subnet_id":  subnet.ID,
		"network_id": subnet.NetworkID,
	}
}

func subnetOptions(subnet networkkit.Subnet) map[string]string {
	if !subnet.EnableDHCP {
		return nil
	}
	options := map[string]string{
		"lease_time": "3600",
		"server_mac": "fa:16:3e:00:00:01",
	}
	if subnet.GatewayIP != "" {
		options["server_id"] = subnet.GatewayIP
		options["router"] = subnet.GatewayIP
	}
	return options
}

func portExternalIDs(port networkkit.Port) map[string]string {
	ids := map[string]string{
		"orion-port-id": port.ID,
	}
	if port.DeviceID != "" {
		ids["orion-device-id"] = port.DeviceID
	}
	if port.DeviceOwner != "" {
		ids["orion-device-owner"] = port.DeviceOwner
	}
	if port.BindingHostID != "" {
		ids["orion-binding-host-id"] = port.BindingHostID
	}
	return ids
}

func namedUUID(prefix, value string) string {
	clean := strings.NewReplacer("-", "_", ".", "_", ":", "_").Replace(value)
	return prefix + "_" + clean
}

func ovnPortName(portID string) string {
	if _, value, ok := strings.Cut(portID, "_"); ok {
		return value
	}
	return portID
}
