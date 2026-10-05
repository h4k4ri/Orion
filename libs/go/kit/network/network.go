package network

import "time"

type CreateNetworkRequest struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

type Network struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateSubnetRequest struct {
	ProjectID  string `json:"project_id"`
	NetworkID  string `json:"network_id"`
	Name       string `json:"name"`
	CIDR       string `json:"cidr"`
	GatewayIP  string `json:"gateway_ip,omitempty"`
	EnableDHCP bool   `json:"enable_dhcp"`
}

type Subnet struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	NetworkID       string    `json:"network_id"`
	Name            string    `json:"name"`
	CIDR            string    `json:"cidr"`
	GatewayIP       string    `json:"gateway_ip,omitempty"`
	EnableDHCP      bool      `json:"enable_dhcp"`
	DHCPOptionsUUID string    `json:"dhcp_options_uuid,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateRouterRequest struct {
	ProjectID         string `json:"project_id"`
	Name              string `json:"name"`
	ExternalNetworkID string `json:"external_network_id,omitempty"`
	EnableSNAT        bool   `json:"enable_snat"`
	SNATExternalIP    string `json:"snat_external_ip,omitempty"`
}

type Router struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	Name              string    `json:"name"`
	ExternalNetworkID string    `json:"external_network_id,omitempty"`
	EnableSNAT        bool      `json:"enable_snat"`
	SNATExternalIP    string    `json:"snat_external_ip,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type RouterInterface struct {
	ID        string    `json:"id"`
	RouterID  string    `json:"router_id"`
	SubnetID  string    `json:"subnet_id"`
	PortID    string    `json:"port_id"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateRouterInterfaceRequest struct {
	RouterID string `json:"router_id"`
	SubnetID string `json:"subnet_id"`
}

type CreateFloatingIPRequest struct {
	ProjectID         string `json:"project_id"`
	FloatingNetworkID string `json:"floating_network_id"`
	FloatingIP        string `json:"floating_ip,omitempty"`
	RouterID          string `json:"router_id,omitempty"`
	PortID            string `json:"port_id,omitempty"`
}

type FloatingIP struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	FloatingNetworkID string    `json:"floating_network_id"`
	FloatingIP        string    `json:"floating_ip"`
	PortID            string    `json:"port_id,omitempty"`
	FixedIP           string    `json:"fixed_ip,omitempty"`
	RouterID          string    `json:"router_id,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type CreatePortRequest struct {
	ProjectID        string   `json:"project_id"`
	NetworkID        string   `json:"network_id"`
	DeviceID         string   `json:"device_id,omitempty"`
	DeviceOwner      string   `json:"device_owner,omitempty"`
	BindingHostID    string   `json:"binding_host_id,omitempty"`
	SecurityGroupIDs []string `json:"security_group_ids,omitempty"`
}

type UpdatePortBindingRequest struct {
	BindingHostID string `json:"binding_host_id"`
	BindingStatus string `json:"binding_status"`
	BindingDetail string `json:"binding_detail,omitempty"`
}

type FixedIP struct {
	SubnetID  string `json:"subnet_id"`
	IPAddress string `json:"ip_address"`
}

type Port struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	NetworkID        string    `json:"network_id"`
	DeviceID         string    `json:"device_id,omitempty"`
	DeviceOwner      string    `json:"device_owner,omitempty"`
	BindingHostID    string    `json:"binding_host_id,omitempty"`
	BindingStatus    string    `json:"binding_status,omitempty"`
	BindingDetail    string    `json:"binding_detail,omitempty"`
	MACAddress       string    `json:"mac_address"`
	FixedIPs         []FixedIP `json:"fixed_ips"`
	Status           string    `json:"status"`
	VIFType          string    `json:"vif_type"`
	VNICType         string    `json:"vnic_type"`
	SecurityGroupIDs []string  `json:"security_group_ids,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SecurityGroup struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateSecurityGroupRequest struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SecurityGroupRule struct {
	ID              string    `json:"id"`
	SecurityGroupID string    `json:"security_group_id"`
	Direction       string    `json:"direction"`
	EtherType       string    `json:"ether_type"`
	Protocol        string    `json:"protocol,omitempty"`
	PortMin         int       `json:"port_min,omitempty"`
	PortMax         int       `json:"port_max,omitempty"`
	RemoteCIDR      string    `json:"remote_cidr,omitempty"`
	RemoteGroupID   string    `json:"remote_group_id,omitempty"`
	Description     string    `json:"description,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type CreateSecurityGroupRuleRequest struct {
	SecurityGroupID string `json:"security_group_id"`
	Direction       string `json:"direction"`
	EtherType       string `json:"ether_type"`
	Protocol        string `json:"protocol,omitempty"`
	PortMin         int    `json:"port_min,omitempty"`
	PortMax         int    `json:"port_max,omitempty"`
	RemoteCIDR      string `json:"remote_cidr,omitempty"`
	RemoteGroupID   string `json:"remote_group_id,omitempty"`
	Description     string `json:"description,omitempty"`
}
