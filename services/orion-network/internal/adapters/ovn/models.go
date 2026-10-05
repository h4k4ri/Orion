package ovn

type logicalSwitch struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Ports       []string          `ovsdb:"ports"`
	ACLs        []string          `ovsdb:"acls"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

type acl struct {
	UUID        string            `ovsdb:"_uuid"`
	Direction   string            `ovsdb:"direction"`
	Priority    int               `ovsdb:"priority"`
	Match       string            `ovsdb:"match"`
	Action      string            `ovsdb:"action"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

type dhcpOptions struct {
	UUID        string            `ovsdb:"_uuid"`
	CIDR        string            `ovsdb:"cidr"`
	Options     map[string]string `ovsdb:"options"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

type logicalSwitchPort struct {
	UUID          string            `ovsdb:"_uuid"`
	Name          string            `ovsdb:"name"`
	Addresses     []string          `ovsdb:"addresses"`
	PortSecurity  []string          `ovsdb:"port_security"`
	DHCPv4Options *string           `ovsdb:"dhcpv4_options"`
	ExternalIDs   map[string]string `ovsdb:"external_ids"`
	Type          string            `ovsdb:"type"`
	Options       map[string]string `ovsdb:"options"`
	Up            *bool             `ovsdb:"up"`
}

type logicalRouter struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Ports       []string          `ovsdb:"ports"`
	NAT         []string          `ovsdb:"nat"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

type logicalRouterPort struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	MAC         string            `ovsdb:"mac"`
	Networks    []string          `ovsdb:"networks"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}

type nat struct {
	UUID        string            `ovsdb:"_uuid"`
	Type        string            `ovsdb:"type"`
	LogicalIP   string            `ovsdb:"logical_ip"`
	ExternalIP  string            `ovsdb:"external_ip"`
	LogicalPort *string           `ovsdb:"logical_port"`
	ExternalIDs map[string]string `ovsdb:"external_ids"`
}
