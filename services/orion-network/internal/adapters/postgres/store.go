package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	networkkit "github.com/horizon/orion/libs/go/kit/network"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/services/orion-network/internal/ports"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store implements ports.NetworkStore using PostgreSQL.
type Store struct {
	pool   *pgxpool.Pool
	schema string
}

func (s *Store) q(t string) string {
	return s.schema + "." + t
}

// New runs schema migration and returns a ready Store.
func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := kitpg.ApplyMigrations(ctx, pool, "orion-network", migrationsFS, "migrations"); err != nil {
		return nil, err
	}
	return &Store{pool: pool, schema: "orion_network"}, nil
}

func (s *Store) AllocateProjectQuota(ctx context.Context, projectID string, networks, portCount int) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_network.project_quotas(project_id) VALUES ($1) ON CONFLICT DO NOTHING`, projectID)
	if err != nil {
		return err
	}
	var allocated int
	err = s.pool.QueryRow(ctx, `UPDATE orion_network.project_quotas SET networks_used=networks_used+$2, ports_used=ports_used+$3, updated_at=NOW() WHERE project_id=$1 AND networks_used+$2<=networks_limit AND ports_used+$3<=ports_limit RETURNING 1`, projectID, networks, portCount).Scan(&allocated)
	if err == pgx.ErrNoRows {
		return ports.ErrQuotaExceeded
	}
	return err
}

func (s *Store) ReleaseProjectQuota(ctx context.Context, projectID string, networks, portCount int) error {
	_, err := s.pool.Exec(ctx, `UPDATE orion_network.project_quotas SET networks_used=GREATEST(0, networks_used-$2), ports_used=GREATEST(0, ports_used-$3), updated_at=NOW() WHERE project_id=$1`, projectID, networks, portCount)
	return err
}

func (s *Store) EnsureFinalizers(ctx context.Context, resourceType, resourceID string, finalizers []string) error {
	for _, finalizer := range finalizers {
		if _, err := s.pool.Exec(ctx, `INSERT INTO orion_network.resource_finalizers(resource_type, resource_id, finalizer) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, resourceType, resourceID, finalizer); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RemoveFinalizer(ctx context.Context, resourceType, resourceID, finalizer string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_network.resource_finalizers WHERE resource_type=$1 AND resource_id=$2 AND finalizer=$3`, resourceType, resourceID, finalizer)
	return err
}

func (s *Store) ListFinalizers(ctx context.Context, resourceType, resourceID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT finalizer FROM orion_network.resource_finalizers WHERE resource_type=$1 AND resource_id=$2 ORDER BY finalizer`, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var finalizer string
		if err := rows.Scan(&finalizer); err != nil {
			return nil, err
		}
		result = append(result, finalizer)
	}
	return result, rows.Err()
}

// ── networks ──────────────────────────────────────────────────────────────────

func (s *Store) SaveNetwork(ctx context.Context, n networkkit.Network) error {
	const q = `
		INSERT INTO orion_network.networks (id, project_id, name, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (id) DO UPDATE SET
			project_id = EXCLUDED.project_id,
			name       = EXCLUDED.name,
			status     = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, q, n.ID, n.ProjectID, n.Name, n.Status, n.CreatedAt, n.UpdatedAt)
	return err
}

func (s *Store) GetNetwork(ctx context.Context, id string) (networkkit.Network, error) {
	const q = `SELECT id, project_id, name, status, created_at, updated_at FROM orion_network.networks WHERE id = $1`
	var n networkkit.Network
	var ca, ua time.Time
	err := s.pool.QueryRow(ctx, q, id).Scan(&n.ID, &n.ProjectID, &n.Name, &n.Status, &ca, &ua)
	if errors.Is(err, pgx.ErrNoRows) {
		return networkkit.Network{}, ports.ErrNetworkRecordNotFound
	}
	n.CreatedAt, n.UpdatedAt = ca, ua
	return n, err
}

func (s *Store) ListNetworks(ctx context.Context) ([]networkkit.Network, error) {
	const q = `SELECT id, project_id, name, status, created_at, updated_at FROM orion_network.networks ORDER BY created_at`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []networkkit.Network
	for rows.Next() {
		var n networkkit.Network
		var ca, ua time.Time
		if err := rows.Scan(&n.ID, &n.ProjectID, &n.Name, &n.Status, &ca, &ua); err != nil {
			return nil, err
		}
		n.CreatedAt, n.UpdatedAt = ca, ua
		items = append(items, n)
	}
	return items, rows.Err()
}

func (s *Store) DeleteNetwork(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_network.networks WHERE id = $1`, id)
	return err
}

// ── subnets ───────────────────────────────────────────────────────────────────

func (s *Store) SaveSubnet(ctx context.Context, sub networkkit.Subnet) error {
	const q = `
		INSERT INTO orion_network.subnets
			(id, project_id, network_id, name, cidr, gateway_ip, enable_dhcp, dhcp_options_uuid, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET
			project_id        = EXCLUDED.project_id,
			name              = EXCLUDED.name,
			cidr              = EXCLUDED.cidr,
			gateway_ip        = EXCLUDED.gateway_ip,
			enable_dhcp       = EXCLUDED.enable_dhcp,
			dhcp_options_uuid = EXCLUDED.dhcp_options_uuid,
			updated_at        = EXCLUDED.updated_at`
	_, err := s.pool.Exec(ctx, q,
		sub.ID, sub.ProjectID, sub.NetworkID, sub.Name, sub.CIDR,
		sub.GatewayIP, sub.EnableDHCP, sub.DHCPOptionsUUID,
		sub.CreatedAt, sub.UpdatedAt,
	)
	return err
}

func (s *Store) GetSubnet(ctx context.Context, id string) (networkkit.Subnet, error) {
	const q = `SELECT id, project_id, network_id, name, cidr, gateway_ip, enable_dhcp, dhcp_options_uuid, created_at, updated_at FROM orion_network.subnets WHERE id = $1`
	var sub networkkit.Subnet
	var ca, ua time.Time
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&sub.ID, &sub.ProjectID, &sub.NetworkID, &sub.Name, &sub.CIDR,
		&sub.GatewayIP, &sub.EnableDHCP, &sub.DHCPOptionsUUID, &ca, &ua,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return networkkit.Subnet{}, ports.ErrSubnetRecordNotFound
	}
	sub.CreatedAt, sub.UpdatedAt = ca, ua
	return sub, err
}

func (s *Store) ListSubnets(ctx context.Context) ([]networkkit.Subnet, error) {
	const q = `SELECT id, project_id, network_id, name, cidr, gateway_ip, enable_dhcp, dhcp_options_uuid, created_at, updated_at FROM orion_network.subnets ORDER BY created_at`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []networkkit.Subnet
	for rows.Next() {
		var sub networkkit.Subnet
		var ca, ua time.Time
		if err := rows.Scan(
			&sub.ID, &sub.ProjectID, &sub.NetworkID, &sub.Name, &sub.CIDR,
			&sub.GatewayIP, &sub.EnableDHCP, &sub.DHCPOptionsUUID, &ca, &ua,
		); err != nil {
			return nil, err
		}
		sub.CreatedAt, sub.UpdatedAt = ca, ua
		items = append(items, sub)
	}
	return items, rows.Err()
}

func (s *Store) FirstSubnetByNetwork(ctx context.Context, networkID string) (networkkit.Subnet, bool) {
	const q = `SELECT id, project_id, network_id, name, cidr, gateway_ip, enable_dhcp, dhcp_options_uuid, created_at, updated_at FROM orion_network.subnets WHERE network_id = $1 LIMIT 1`
	var sub networkkit.Subnet
	var ca, ua time.Time
	err := s.pool.QueryRow(ctx, q, networkID).Scan(
		&sub.ID, &sub.ProjectID, &sub.NetworkID, &sub.Name, &sub.CIDR,
		&sub.GatewayIP, &sub.EnableDHCP, &sub.DHCPOptionsUUID, &ca, &ua,
	)
	if err != nil {
		return networkkit.Subnet{}, false
	}
	sub.CreatedAt, sub.UpdatedAt = ca, ua
	return sub, true
}

// ── ports ─────────────────────────────────────────────────────────────────────

func (s *Store) SavePort(ctx context.Context, p networkkit.Port) error {
	fixedIPs, err := json.Marshal(p.FixedIPs)
	if err != nil {
		return err
	}
	securityGroups, err := json.Marshal(p.SecurityGroupIDs)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO orion_network.ports
			(id, project_id, network_id, device_id, device_owner, binding_host_id, binding_status, binding_detail,
			 mac_address, fixed_ips, security_group_ids, status, vif_type, vnic_type, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (id) DO UPDATE SET
			project_id      = EXCLUDED.project_id,
			device_id       = EXCLUDED.device_id,
			device_owner    = EXCLUDED.device_owner,
			binding_host_id = EXCLUDED.binding_host_id,
			binding_status  = EXCLUDED.binding_status,
			binding_detail  = EXCLUDED.binding_detail,
			mac_address     = EXCLUDED.mac_address,
			 fixed_ips       = EXCLUDED.fixed_ips,
			 security_group_ids = EXCLUDED.security_group_ids,
			status          = EXCLUDED.status,
			vif_type        = EXCLUDED.vif_type,
			vnic_type       = EXCLUDED.vnic_type,
			updated_at      = EXCLUDED.updated_at`
	_, err = s.pool.Exec(ctx, q,
		p.ID, p.ProjectID, p.NetworkID, p.DeviceID, p.DeviceOwner, p.BindingHostID, p.BindingStatus, p.BindingDetail,
		p.MACAddress, fixedIPs, securityGroups, p.Status, p.VIFType, p.VNICType,
		p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (s *Store) GetPort(ctx context.Context, id string) (networkkit.Port, error) {
	const q = `SELECT id, project_id, network_id, device_id, device_owner, binding_host_id, binding_status, binding_detail, mac_address, fixed_ips, security_group_ids, status, vif_type, vnic_type, created_at, updated_at FROM orion_network.ports WHERE id = $1`
	p, err := scanPort(s.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return networkkit.Port{}, ports.ErrPortRecordNotFound
	}
	return p, err
}

func (s *Store) ListPorts(ctx context.Context) ([]networkkit.Port, error) {
	const q = `SELECT id, project_id, network_id, device_id, device_owner, binding_host_id, binding_status, binding_detail, mac_address, fixed_ips, security_group_ids, status, vif_type, vnic_type, created_at, updated_at FROM orion_network.ports ORDER BY created_at`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []networkkit.Port
	for rows.Next() {
		p, err := scanPort(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (s *Store) DeletePort(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_network.ports WHERE id = $1`, id)
	return err
}

func (s *Store) UsedIPsBySubnet(ctx context.Context, subnetID string) (map[string]struct{}, error) {
	const q = `SELECT fixed_ips FROM orion_network.ports WHERE fixed_ips @> $1`
	filter, _ := json.Marshal([]map[string]string{{"subnet_id": subnetID}})
	rows, err := s.pool.Query(ctx, q, filter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	used := map[string]struct{}{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var fixedIPs []networkkit.FixedIP
		if err := json.Unmarshal(raw, &fixedIPs); err != nil {
			continue
		}
		for _, fip := range fixedIPs {
			if fip.SubnetID == subnetID {
				used[fip.IPAddress] = struct{}{}
			}
		}
	}
	return used, rows.Err()
}

// ── helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanPort(row scanner) (networkkit.Port, error) {
	var p networkkit.Port
	var fixedIPsRaw []byte
	var securityGroupsRaw []byte
	var ca, ua time.Time
	err := row.Scan(
		&p.ID, &p.ProjectID, &p.NetworkID, &p.DeviceID, &p.DeviceOwner, &p.BindingHostID, &p.BindingStatus, &p.BindingDetail,
		&p.MACAddress, &fixedIPsRaw, &securityGroupsRaw, &p.Status, &p.VIFType, &p.VNICType, &ca, &ua,
	)
	if err != nil {
		return networkkit.Port{}, err
	}
	_ = json.Unmarshal(fixedIPsRaw, &p.FixedIPs)
	_ = json.Unmarshal(securityGroupsRaw, &p.SecurityGroupIDs)
	p.CreatedAt, p.UpdatedAt = ca, ua
	return p, nil
}

func (s *Store) SaveSecurityGroup(ctx context.Context, group networkkit.SecurityGroup) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_network.security_groups(id, project_id, name, description, created_at, updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET project_id=EXCLUDED.project_id, name=EXCLUDED.name, description=EXCLUDED.description, updated_at=EXCLUDED.updated_at`, group.ID, group.ProjectID, group.Name, group.Description, group.CreatedAt, group.UpdatedAt)
	return err
}

func (s *Store) GetSecurityGroup(ctx context.Context, id string) (networkkit.SecurityGroup, error) {
	var group networkkit.SecurityGroup
	err := s.pool.QueryRow(ctx, `SELECT id, project_id, name, description, created_at, updated_at FROM orion_network.security_groups WHERE id=$1`, id).Scan(&group.ID, &group.ProjectID, &group.Name, &group.Description, &group.CreatedAt, &group.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return networkkit.SecurityGroup{}, ports.ErrSecurityGroupRecordNotFound
	}
	return group, err
}

func (s *Store) ListSecurityGroups(ctx context.Context, projectID string) ([]networkkit.SecurityGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, project_id, name, description, created_at, updated_at FROM orion_network.security_groups WHERE ($1='' OR project_id=$1) ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]networkkit.SecurityGroup, 0)
	for rows.Next() {
		var group networkkit.SecurityGroup
		if err := rows.Scan(&group.ID, &group.ProjectID, &group.Name, &group.Description, &group.CreatedAt, &group.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, group)
	}
	return items, rows.Err()
}

func (s *Store) DeleteSecurityGroup(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_network.security_groups WHERE id=$1`, id)
	return err
}

func (s *Store) SaveSecurityGroupRule(ctx context.Context, rule networkkit.SecurityGroupRule) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_network.security_group_rules(id, security_group_id, direction, ether_type, protocol, port_min, port_max, remote_cidr, remote_group_id, description, created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(id) DO UPDATE SET direction=EXCLUDED.direction, ether_type=EXCLUDED.ether_type, protocol=EXCLUDED.protocol, port_min=EXCLUDED.port_min, port_max=EXCLUDED.port_max, remote_cidr=EXCLUDED.remote_cidr, remote_group_id=EXCLUDED.remote_group_id, description=EXCLUDED.description`, rule.ID, rule.SecurityGroupID, rule.Direction, rule.EtherType, rule.Protocol, rule.PortMin, rule.PortMax, rule.RemoteCIDR, rule.RemoteGroupID, rule.Description, rule.CreatedAt)
	return err
}

func (s *Store) GetSecurityGroupRule(ctx context.Context, id string) (networkkit.SecurityGroupRule, error) {
	var rule networkkit.SecurityGroupRule
	err := s.pool.QueryRow(ctx, `SELECT id, security_group_id, direction, ether_type, protocol, port_min, port_max, remote_cidr, remote_group_id, description, created_at FROM orion_network.security_group_rules WHERE id=$1`, id).Scan(&rule.ID, &rule.SecurityGroupID, &rule.Direction, &rule.EtherType, &rule.Protocol, &rule.PortMin, &rule.PortMax, &rule.RemoteCIDR, &rule.RemoteGroupID, &rule.Description, &rule.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return networkkit.SecurityGroupRule{}, ports.ErrSecurityGroupRuleRecordNotFound
	}
	return rule, err
}

func (s *Store) ListSecurityGroupRules(ctx context.Context, groupID string) ([]networkkit.SecurityGroupRule, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, security_group_id, direction, ether_type, protocol, port_min, port_max, remote_cidr, remote_group_id, description, created_at FROM orion_network.security_group_rules WHERE ($1='' OR security_group_id=$1) ORDER BY created_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]networkkit.SecurityGroupRule, 0)
	for rows.Next() {
		var rule networkkit.SecurityGroupRule
		if err := rows.Scan(&rule.ID, &rule.SecurityGroupID, &rule.Direction, &rule.EtherType, &rule.Protocol, &rule.PortMin, &rule.PortMax, &rule.RemoteCIDR, &rule.RemoteGroupID, &rule.Description, &rule.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, rule)
	}
	return items, rows.Err()
}

func (s *Store) DeleteSecurityGroupRule(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_network.security_group_rules WHERE id=$1`, id)
	return err
}

func (s *Store) SaveRouter(ctx context.Context, router networkkit.Router) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_network.routers(id, project_id, name, external_network_id, enable_snat, snat_external_ip, status, created_at, updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET project_id=EXCLUDED.project_id, name=EXCLUDED.name, external_network_id=EXCLUDED.external_network_id, enable_snat=EXCLUDED.enable_snat, snat_external_ip=EXCLUDED.snat_external_ip, status=EXCLUDED.status, updated_at=EXCLUDED.updated_at`, router.ID, router.ProjectID, router.Name, router.ExternalNetworkID, router.EnableSNAT, router.SNATExternalIP, router.Status, router.CreatedAt, router.UpdatedAt)
	return err
}

func (s *Store) GetRouter(ctx context.Context, id string) (networkkit.Router, error) {
	var item networkkit.Router
	err := s.pool.QueryRow(ctx, `SELECT id, project_id, name, external_network_id, enable_snat, snat_external_ip, status, created_at, updated_at FROM orion_network.routers WHERE id=$1`, id).Scan(&item.ID, &item.ProjectID, &item.Name, &item.ExternalNetworkID, &item.EnableSNAT, &item.SNATExternalIP, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return networkkit.Router{}, ports.ErrRouterRecordNotFound
	}
	return item, err
}

func (s *Store) ListRouters(ctx context.Context, projectID string) ([]networkkit.Router, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, project_id, name, external_network_id, enable_snat, snat_external_ip, status, created_at, updated_at FROM orion_network.routers WHERE ($1='' OR project_id=$1) ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]networkkit.Router, 0)
	for rows.Next() {
		var item networkkit.Router
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Name, &item.ExternalNetworkID, &item.EnableSNAT, &item.SNATExternalIP, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteRouter(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_network.routers WHERE id=$1`, id)
	return err
}

func (s *Store) SaveRouterInterface(ctx context.Context, item networkkit.RouterInterface) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_network.router_interfaces(id, router_id, subnet_id, port_id, created_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET subnet_id=EXCLUDED.subnet_id, port_id=EXCLUDED.port_id`, item.ID, item.RouterID, item.SubnetID, item.PortID, item.CreatedAt)
	return err
}
func (s *Store) GetRouterInterface(ctx context.Context, id string) (networkkit.RouterInterface, error) {
	var item networkkit.RouterInterface
	err := s.pool.QueryRow(ctx, `SELECT id, router_id, subnet_id, port_id, created_at FROM orion_network.router_interfaces WHERE id=$1`, id).Scan(&item.ID, &item.RouterID, &item.SubnetID, &item.PortID, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return networkkit.RouterInterface{}, ports.ErrRouterInterfaceRecordNotFound
	}
	return item, err
}
func (s *Store) ListRouterInterfaces(ctx context.Context, routerID string) ([]networkkit.RouterInterface, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, router_id, subnet_id, port_id, created_at FROM orion_network.router_interfaces WHERE ($1='' OR router_id=$1) ORDER BY created_at`, routerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]networkkit.RouterInterface, 0)
	for rows.Next() {
		var item networkkit.RouterInterface
		if err := rows.Scan(&item.ID, &item.RouterID, &item.SubnetID, &item.PortID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) DeleteRouterInterface(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_network.router_interfaces WHERE id=$1`, id)
	return err
}

func (s *Store) SaveFloatingIP(ctx context.Context, item networkkit.FloatingIP) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO orion_network.floating_ips(id, project_id, floating_network_id, floating_ip, port_id, fixed_ip, router_id, status, created_at, updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET port_id=EXCLUDED.port_id, fixed_ip=EXCLUDED.fixed_ip, router_id=EXCLUDED.router_id, status=EXCLUDED.status, updated_at=EXCLUDED.updated_at`, item.ID, item.ProjectID, item.FloatingNetworkID, item.FloatingIP, item.PortID, item.FixedIP, item.RouterID, item.Status, item.CreatedAt, item.UpdatedAt)
	return err
}
func (s *Store) GetFloatingIP(ctx context.Context, id string) (networkkit.FloatingIP, error) {
	var item networkkit.FloatingIP
	err := s.pool.QueryRow(ctx, `SELECT id, project_id, floating_network_id, floating_ip, port_id, fixed_ip, router_id, status, created_at, updated_at FROM orion_network.floating_ips WHERE id=$1`, id).Scan(&item.ID, &item.ProjectID, &item.FloatingNetworkID, &item.FloatingIP, &item.PortID, &item.FixedIP, &item.RouterID, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return networkkit.FloatingIP{}, ports.ErrFloatingIPRecordNotFound
	}
	return item, err
}
func (s *Store) ListFloatingIPs(ctx context.Context, projectID string) ([]networkkit.FloatingIP, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, project_id, floating_network_id, floating_ip, port_id, fixed_ip, router_id, status, created_at, updated_at FROM orion_network.floating_ips WHERE ($1='' OR project_id=$1) ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]networkkit.FloatingIP, 0)
	for rows.Next() {
		var item networkkit.FloatingIP
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.FloatingNetworkID, &item.FloatingIP, &item.PortID, &item.FixedIP, &item.RouterID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) DeleteFloatingIP(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_network.floating_ips WHERE id=$1`, id)
	return err
}
