package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/horizon/orion/sdk/go/plugin"
)

func main() {
	p := plugin.New(plugin.Config{
		ID:      "network-plugin",
		Name:    "Network",
		Version: "1.0.0",
		Vendor:  "OVN/OVS",
	})

	p.Resource("orion.io/network.network", "v1").
		Handle("create", handleNetworkCreate).
		Handle("delete", handleNetworkDelete).
		Handle("get", handleNetworkGet).
		Handle("list", handleNetworkList).
		Handle("set_external", handleNetworkSetExternal).
		Register()

	p.Resource("orion.io/network.subnet", "v1").
		Handle("create", handleSubnetCreate).
		Handle("delete", handleSubnetDelete).
		Handle("get", handleSubnetGet).
		Handle("list", handleSubnetList).
		Handle("add_dhcp", handleSubnetAddDHCP).
		Register()

	p.Resource("orion.io/network.port", "v1").
		Handle("create", handlePortCreate).
		Handle("delete", handlePortDelete).
		Handle("get", handlePortGet).
		Handle("list", handlePortList).
		Handle("set_port_security", handlePortSetSecurity).
		Handle("bind", handlePortBind).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50063"
	}
	log.Printf("Network plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func runOVN(args ...string) (string, error) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "ovn-nbctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("ovn-nbctl %v failed: %w, output: %s", args, err, string(output))
	}
	return string(output), nil
}

func runOVS(args ...string) (string, error) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "ovs-vsctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("ovs-vsctl %v failed: %w, output: %s", args, err, string(output))
	}
	return string(output), nil
}

func handleNetworkCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	cidr, _ := input["cidr"].(string)
	external, _ := input["external"].(bool)
	router, _ := input["router"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	lsName := name
	if cidr != "" {
		cmd := exec.CommandContext(ctx, "ovn-nbctl", "ls-add", lsName)
		cmd.Run()

		gwAddr := strings.Split(cidr, "/")[0]
		cmd = exec.CommandContext(ctx, "ovn-nbctl", "set", "logical_switch", lsName, fmt.Sprintf("external_ids:gateway_ip=%s", gwAddr))
		cmd.Run()

		cmd = exec.CommandContext(ctx, "ovn-nbctl", "lsd-address", lsName, cidr)
		cmd.Run()
	}

	if external {
		runOVN("set", "logical_switch", lsName, "external_ids:ovn-localnet-lb=true")
	}

	if router != "" {
		runOVN("lsr-add", router, "--logical_switch", lsName)
	}

	log.Printf("Network: created network %s (cidr=%s, external=%v)", name, cidr, external)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":       lsName,
			"name":     name,
			"cidr":     cidr,
			"external": external,
			"status":   "created",
		}),
	}, nil
}

func handleNetworkDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	runOVN("ls-del", name)
	log.Printf("Network: deleted network %s", name)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleNetworkGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	output, _ := runOVN("ls-list", name)
	log.Printf("Network: got network %s", name)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"output": output,
		}),
	}, nil
}

func handleNetworkList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	output, _ := runOVN("ls-list")
	lines := strings.Split(output, "\n")
	var networks []map[string]interface{}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			name := strings.TrimSpace(line)
			networks = append(networks, map[string]interface{}{"id": name, "name": name})
		}
	}

	log.Printf("Network: listed %d networks", len(networks))
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"networks": networks}),
	}, nil
}

func handleNetworkSetExternal(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	external, _ := input["external"].(bool)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	if external {
		runOVN("set", "logical_switch", name, "external_ids:ovn-localnet-lb=true")
	}

	log.Printf("Network: set external=%v for network %s", external, name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "external": external}),
	}, nil
}

func handleSubnetCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	network, _ := input["network"].(string)
	cidr, _ := input["cidr"].(string)
	gateway, _ := input["gateway"].(string)
	dhcp, _ := input["enable_dhcp"].(bool)

	if network == "" || cidr == "" {
		return errorResponse(fmt.Errorf("network and cidr are required")), nil
	}

	cmd := exec.CommandContext(ctx, "ovn-nbctl", "lsp-add", network, cidr)
	cmd.Run()

	if gateway != "" {
		cmd = exec.CommandContext(ctx, "ovn-nbctl", "lsp-set-gateway-chassis", cidr, gateway)
		cmd.Run()
	}

	if dhcp {
		runOVN("lsp-set-dhcpv4-options", cidr, fmt.Sprintf("{\"server_id\":\"%s\",\"server_addr\":\"%s\",\"lease_time\":3600,\"router\":\"%s\"}", gateway, gateway, gateway))
	}

	log.Printf("Subnet: created subnet %s in network %s (gateway=%s, dhcp=%v)", cidr, network, gateway, dhcp)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":      cidr,
			"network": network,
			"cidr":    cidr,
			"gateway": gateway,
			"dhcp":    dhcp,
			"status":  "created",
		}),
	}, nil
}

func handleSubnetDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	network, _ := input["network"].(string)
	cidr, _ := input["cidr"].(string)

	if network == "" || cidr == "" {
		return errorResponse(fmt.Errorf("network and cidr are required")), nil
	}

	runOVN("lsp-del", cidr)
	log.Printf("Subnet: deleted subnet %s from network %s", cidr, network)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleSubnetGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	cidr, _ := input["cidr"].(string)
	log.Printf("Subnet: got subnet %s", cidr)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": cidr, "cidr": cidr}),
	}, nil
}

func handleSubnetList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	network, _ := input["network"].(string)
	if network == "" {
		return errorResponse(fmt.Errorf("network is required")), nil
	}

	output, _ := runOVN("lsp-list", network)
	log.Printf("Subnet: listed subnets in network %s", network)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"network": network,
			"output":  output,
		}),
	}, nil
}

func handleSubnetAddDHCP(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	port, _ := input["port"].(string)
	cidr, _ := input["cidr"].(string)
	serverIP, _ := input["server_ip"].(string)
	rangeStart, _ := input["range_start"].(string)
	rangeEnd, _ := input["range_end"].(string)

	if port == "" || cidr == "" {
		return errorResponse(fmt.Errorf("port and cidr are required")), nil
	}

	dhcpOptions := fmt.Sprintf("{\"server_id\":\"%s\",\"server_addr\":\"%s\",\"lease_time\":3600,\"router\":\"%s\"", serverIP, serverIP, serverIP)
	if rangeStart != "" && rangeEnd != "" {
		dhcpOptions += fmt.Sprintf(",\"dynamic_start\":\"%s\",\"dynamic_end\":\"%s\"", rangeStart, rangeEnd)
	}
	dhcpOptions += "}"

	runOVN("lsp-set-dhcpv4-options", port, dhcpOptions)

	log.Printf("Subnet: added DHCP to port %s (server=%s)", port, serverIP)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"port":        port,
			"cidr":        cidr,
			"server_ip":   serverIP,
			"range_start": rangeStart,
			"range_end":   rangeEnd,
		}),
	}, nil
}

func handlePortCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	network, _ := input["network"].(string)
	name, _ := input["name"].(string)
	mac, _ := input["mac_address"].(string)
	ip, _ := input["ip_address"].(string)

	if network == "" || name == "" {
		return errorResponse(fmt.Errorf("network and name are required")), nil
	}

	runOVN("lsp-add", network, name)

	if mac != "" {
		runOVN("lsp-set-addresses", name, mac)
	} else {
		runOVN("lsp-set-addresses", name, "dynamic")
	}

	if ip != "" {
		runOVN("lsp-set-port-security", name, fmt.Sprintf("%s %s", mac, ip))
	}

	log.Printf("Port: created port %s in network %s (mac=%s, ip=%s)", name, network, mac, ip)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":          name,
			"name":        name,
			"network":     network,
			"mac_address": mac,
			"ip_address":  ip,
			"status":      "created",
		}),
	}, nil
}

func handlePortDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	runOVN("lsp-del", name)
	log.Printf("Port: deleted port %s", name)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handlePortGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	output, _ := runOVN("lsp-get", name)
	log.Printf("Port: got port %s", name)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "name": name, "output": output}),
	}, nil
}

func handlePortList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	network, _ := input["network"].(string)
	if network == "" {
		return errorResponse(fmt.Errorf("network is required")), nil
	}

	output, _ := runOVN("lsp-list", network)
	log.Printf("Port: listed ports in network %s", network)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"network": network, "output": output}),
	}, nil
}

func handlePortSetSecurity(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	mac, _ := input["mac_address"].(string)
	ip, _ := input["ip_address"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	security := fmt.Sprintf("%s %s", mac, ip)
	runOVN("lsp-set-port-security", name, security)

	log.Printf("Port: set port security for %s (mac=%s, ip=%s)", name, mac, ip)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "mac_address": mac, "ip_address": ip}),
	}, nil
}

func handlePortBind(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	port, _ := input["port"].(string)
	chassis, _ := input["chassis"].(string)

	if port == "" {
		return errorResponse(fmt.Errorf("port is required")), nil
	}

	runOVN("lsp-set-bindings", port, chassis)

	log.Printf("Port: bound port %s to chassis %s", port, chassis)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": port, "chassis": chassis}),
	}, nil
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "NETWORK_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
