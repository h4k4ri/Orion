//go:build integration

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	orioncli "github.com/horizon/orion/cli/orion-cli/internal/cli"
	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	libovsdbclient "github.com/ovn-org/libovsdb/client"
	"github.com/ovn-org/libovsdb/model"
	"libvirt.org/go/libvirt"
	"libvirt.org/go/libvirtxml"
)

const ovnNBDBName = "OVN_Northbound"

type ovnLogicalSwitchPort struct {
	UUID      string   `ovsdb:"_uuid"`
	Name      string   `ovsdb:"name"`
	Addresses []string `ovsdb:"addresses"`
	Up        *bool    `ovsdb:"up"`
}

type e2eConfig struct {
	apiURL       string
	identityURL  string
	placementURL string
	imageURL     string
	networkURL   string
	nodeAgentURL string
	volumeURL    string
	volumeHost   string
	ovnNBDB      string

	username  string
	password  string
	projectID string
	imageID   string
	flavor    string

	volumeMode    string
	keepResources bool
	timeout       time.Duration
}

type e2eState struct {
	client *orioncli.Client
	cfg    e2eConfig

	tokenValue string
	network    networkkit.Network
	subnet     networkkit.Subnet
	serverAID  string
	serverBID  string
	volumeID   string
}

type consoleSession struct {
	conn   *libvirt.Connect
	domain *libvirt.Domain
	stream *libvirt.Stream

	mu  sync.Mutex
	buf bytes.Buffer
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "e2e failed: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() e2eConfig {
	var cfg e2eConfig
	flag.StringVar(&cfg.apiURL, "api-url", envOr("ORION_API_URL", "http://127.0.0.1:8080"), "orion-api base URL")
	flag.StringVar(&cfg.identityURL, "identity-url", envOr("ORION_IDENTITY_URL", "http://127.0.0.1:8081"), "orion-identity base URL")
	flag.StringVar(&cfg.placementURL, "placement-url", envOr("ORION_PLACEMENT_URL", "http://127.0.0.1:8082"), "orion-placement base URL")
	flag.StringVar(&cfg.imageURL, "image-url", envOr("ORION_IMAGE_URL", "http://127.0.0.1:8085"), "orion-image base URL")
	flag.StringVar(&cfg.networkURL, "network-url", envOr("ORION_NETWORK_URL", "http://127.0.0.1:8086"), "orion-network base URL")
	flag.StringVar(&cfg.nodeAgentURL, "node-agent-url", envOr("ORION_NODE_AGENT_URL", "http://127.0.0.1:8084"), "orion-node-agent base URL")
	flag.StringVar(&cfg.volumeURL, "volume-url", envOr("ORION_VOLUME_URL", "http://127.0.0.1:8087"), "orion-volume base URL")
	flag.StringVar(&cfg.volumeHost, "volume-host-agent-url", envOr("ORION_VOLUME_HOST_AGENT_URL", "http://127.0.0.1:8088"), "orion-volume-host-agent base URL")
	flag.StringVar(&cfg.ovnNBDB, "ovn-nb-db", envOr("ORION_E2E_OVN_NB_DB", "tcp:127.0.0.1:6641"), "OVN Northbound DB address")

	flag.StringVar(&cfg.username, "username", envOr("ORION_E2E_USERNAME", "admin"), "username for auth")
	flag.StringVar(&cfg.password, "password", envOr("ORION_E2E_PASSWORD", "orion-admin"), "password for auth")
	flag.StringVar(&cfg.projectID, "project-id", envOr("ORION_E2E_PROJECT_ID", "proj_admin"), "project scope")
	flag.StringVar(&cfg.imageID, "image-id", envOr("ORION_E2E_IMAGE_ID", "img_cirros_0_6_3_x86_64"), "boot image id")
	flag.StringVar(&cfg.flavor, "flavor", envOr("ORION_E2E_FLAVOR", "tiny"), "server flavor")

	flag.StringVar(&cfg.volumeMode, "volume", envOr("ORION_E2E_VOLUME", "auto"), "volume mode: auto, on, off")
	flag.BoolVar(&cfg.keepResources, "keep-resources", false, "keep created resources for debugging")
	flag.DurationVar(&cfg.timeout, "timeout", 3*time.Minute, "global timeout for long waits")
	flag.Parse()
	return cfg
}

func run(cfg e2eConfig) error {
	baseClient := orioncli.NewClient(orioncli.Config{
		APIURL:             cfg.apiURL,
		IdentityURL:        cfg.identityURL,
		PlacementURL:       cfg.placementURL,
		ImageURL:           cfg.imageURL,
		NetworkURL:         cfg.networkURL,
		NodeAgentURL:       cfg.nodeAgentURL,
		VolumeURL:          cfg.volumeURL,
		VolumeHostAgentURL: cfg.volumeHost,
	})

	state := &e2eState{
		client: baseClient,
		cfg:    cfg,
	}

	success := false
	defer func() {
		if success || state.cfg.keepResources {
			return
		}
		if err := state.cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "e2e cleanup warning: %v\n", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	section("Health")
	health := state.client.HealthChecks(ctx)
	printHealth(health)
	for _, item := range health {
		if item.Status != "ok" {
			return fmt.Errorf("health check failed for %s: %s", item.Name, item.Error)
		}
	}

	section("Auth")
	token, err := state.client.IssueToken(ctx, orioncli.AuthenticateRequest{
		Username: cfg.username,
		Password: cfg.password,
		Scope:    mustProjectScope(cfg.projectID),
	})
	if err != nil {
		return fmt.Errorf("issue token: %w", err)
	}
	state.tokenValue = token.Value
	fmt.Printf("token user=%s project=%s expires_at=%s\n", token.Actor.Username, cfg.projectID, token.ExpiresAt.Format(time.RFC3339))

	state.client = orioncli.NewClient(orioncli.Config{
		APIURL:             cfg.apiURL,
		IdentityURL:        cfg.identityURL,
		PlacementURL:       cfg.placementURL,
		ImageURL:           cfg.imageURL,
		NetworkURL:         cfg.networkURL,
		NodeAgentURL:       cfg.nodeAgentURL,
		VolumeURL:          cfg.volumeURL,
		VolumeHostAgentURL: cfg.volumeHost,
		Token:              token.Value,
	})

	suffix := time.Now().UnixNano()
	cidr := pickCIDR(suffix)
	gateway, err := gatewayIP(cidr)
	if err != nil {
		return err
	}

	section("Network")
	state.network, err = state.client.CreateNetwork(ctx, networkkit.CreateNetworkRequest{
		ProjectID: cfg.projectID,
		Name:      fmt.Sprintf("e2e-net-%d", suffix),
	})
	if err != nil {
		return fmt.Errorf("create network: %w", err)
	}
	fmt.Printf("network id=%s name=%s\n", state.network.ID, state.network.Name)

	state.subnet, err = state.client.CreateSubnet(ctx, networkkit.CreateSubnetRequest{
		ProjectID:  cfg.projectID,
		NetworkID:  state.network.ID,
		Name:       fmt.Sprintf("e2e-subnet-%d", suffix),
		CIDR:       cidr,
		GatewayIP:  gateway,
		EnableDHCP: true,
	})
	if err != nil {
		return fmt.Errorf("create subnet: %w", err)
	}
	fmt.Printf("subnet id=%s cidr=%s gateway=%s\n", state.subnet.ID, state.subnet.CIDR, state.subnet.GatewayIP)

	section("Servers")
	serverA, serverB, err := state.createServers(ctx, suffix)
	if err != nil {
		return err
	}
	fmt.Printf("server-a id=%s port=%s\n", serverA.ID, firstOrEmpty(serverA.PortIDs))
	fmt.Printf("server-b id=%s port=%s\n", serverB.ID, firstOrEmpty(serverB.PortIDs))

	portA, err := state.client.GetPort(ctx, firstOrEmpty(serverA.PortIDs))
	if err != nil {
		return fmt.Errorf("get server-a port: %w", err)
	}
	portB, err := state.client.GetPort(ctx, firstOrEmpty(serverB.PortIDs))
	if err != nil {
		return fmt.Errorf("get server-b port: %w", err)
	}
	ipA, err := fixedIP(portA)
	if err != nil {
		return fmt.Errorf("server-a fixed ip: %w", err)
	}
	ipB, err := fixedIP(portB)
	if err != nil {
		return fmt.Errorf("server-b fixed ip: %w", err)
	}
	fmt.Printf("server-a ip=%s mac=%s status=%s\n", ipA, portA.MACAddress, portA.Status)
	fmt.Printf("server-b ip=%s mac=%s status=%s\n", ipB, portB.MACAddress, portB.Status)
	if portA.Status != "active" || portB.Status != "active" {
		return fmt.Errorf("ports not active: a=%s b=%s", portA.Status, portB.Status)
	}

	if err := checkOVNPort(cfg.ovnNBDB, portA, ipA); err != nil {
		return fmt.Errorf("ovn validation server-a: %w", err)
	}
	if err := checkOVNPort(cfg.ovnNBDB, portB, ipB); err != nil {
		return fmt.Errorf("ovn validation server-b: %w", err)
	}
	if err := checkDomainInterface(serverA.ID, portA.MACAddress); err != nil {
		return fmt.Errorf("libvirt interface server-a: %w", err)
	}
	if err := checkDomainInterface(serverB.ID, portB.MACAddress); err != nil {
		return fmt.Errorf("libvirt interface server-b: %w", err)
	}

	section("Guest Access")
	console, err := openConsole(domainName(serverA.ID))
	if err != nil {
		return fmt.Errorf("open serial console: %w", err)
	}
	defer console.Close()

	if err := loginCirros(console, cfg.timeout); err != nil {
		return fmt.Errorf("guest login failed: %w", err)
	}

	guestIP, err := runGuestCommand(console, "ip -4 -o addr show dev eth0 | awk '{print $4}' | cut -d/ -f1", 15*time.Second)
	if err != nil {
		return fmt.Errorf("guest ip inspection failed: %w", err)
	}
	guestIP = lastNonEmptyLine(guestIP)
	fmt.Printf("guest server-a ip=%s\n", guestIP)
	if guestIP != ipA {
		return fmt.Errorf("guest IP mismatch: expected %s got %s", ipA, guestIP)
	}

	routeOutput, err := runGuestCommand(console, "ip route", 15*time.Second)
	if err != nil {
		return fmt.Errorf("guest route inspection failed: %w", err)
	}
	if !strings.Contains(routeOutput, "default via "+gateway) {
		return fmt.Errorf("guest default route missing gateway %s", gateway)
	}
	fmt.Printf("guest default route via %s\n", gateway)

	if err := waitForGuestPing(console, ipB, 45*time.Second); err != nil {
		return fmt.Errorf("guest-to-guest ping failed: %w", err)
	}
	fmt.Printf("guest-to-guest ping succeeded: %s -> %s\n", ipA, ipB)

	section("Volume")
	switch strings.ToLower(cfg.volumeMode) {
	case "off":
		fmt.Println("volume stage skipped by configuration")
	case "auto", "on":
		if err := state.runVolumeStage(ctx, console, serverA.ID); err != nil {
			if strings.ToLower(cfg.volumeMode) == "auto" && isVolumeBackendUnavailable(err) {
				fmt.Printf("volume stage skipped: %v\n", err)
			} else {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid volume mode %q", cfg.volumeMode)
	}

	section("Summary")
	fmt.Println("e2e succeeded")
	fmt.Printf("network=%s subnet=%s server-a=%s server-b=%s\n", state.network.ID, state.subnet.ID, state.serverAID, state.serverBID)
	if state.volumeID != "" {
		fmt.Printf("volume=%s\n", state.volumeID)
	}
	if state.cfg.keepResources {
		fmt.Println("resources kept by request")
	}
	if err := state.cleanup(); err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	success = true
	return nil
}

func (s *e2eState) createServers(ctx context.Context, suffix int64) (compute.Server, compute.Server, error) {
	responseA, err := s.client.CreateServer(ctx, mustServerRequest(
		fmt.Sprintf("e2e-a-%d", suffix),
		s.cfg.imageID,
		s.cfg.flavor,
		s.network.ID,
	))
	if err != nil {
		return compute.Server{}, compute.Server{}, fmt.Errorf("create server-a: %w", err)
	}
	s.serverAID = responseA.Server.ID

	responseB, err := s.client.CreateServer(ctx, mustServerRequest(
		fmt.Sprintf("e2e-b-%d", suffix),
		s.cfg.imageID,
		s.cfg.flavor,
		s.network.ID,
	))
	if err != nil {
		return compute.Server{}, compute.Server{}, fmt.Errorf("create server-b: %w", err)
	}
	s.serverBID = responseB.Server.ID
	return responseA.Server, responseB.Server, nil
}

func (s *e2eState) runVolumeStage(ctx context.Context, console *consoleSession, serverID string) error {
	volume, err := s.client.CreateVolume(ctx, volumekit.CreateVolumeRequest{
		ProjectID: s.cfg.projectID,
		Name:      fmt.Sprintf("e2e-vol-%d", time.Now().UnixNano()),
		SizeGB:    1,
	})
	if err != nil {
		return fmt.Errorf("create volume: %w", err)
	}
	s.volumeID = volume.ID
	fmt.Printf("volume id=%s device=%s status=%s\n", volume.ID, volume.DevicePath, volume.Status)

	if _, err := s.client.AttachVolume(ctx, serverID, volume.ID); err != nil {
		return fmt.Errorf("attach volume: %w", err)
	}
	fmt.Printf("volume attached to %s\n", serverID)

	domain := domainName(serverID)
	if err := waitForDomainBlock(domain, volume.DevicePath, true, 15*time.Second); err != nil {
		return fmt.Errorf("volume not visible in libvirt: %w", err)
	}
	lsblkOutput, err := runGuestCommand(console, "lsblk -o NAME,TYPE | awk '$2 == \"disk\" {print $1}'", 15*time.Second)
	if err != nil {
		return fmt.Errorf("guest lsblk after attach: %w", err)
	}
	if !strings.Contains(lsblkOutput, "vdb") {
		return fmt.Errorf("guest did not expose attached disk vdb; output=%q", strings.TrimSpace(lsblkOutput))
	}
	fmt.Println("guest sees attached disk vdb")

	if _, err := s.client.DetachVolume(ctx, serverID, volume.ID); err != nil {
		return fmt.Errorf("detach volume: %w", err)
	}
	if err := waitForDomainBlock(domain, volume.DevicePath, false, 15*time.Second); err != nil {
		return fmt.Errorf("volume still visible in libvirt after detach: %w", err)
	}
	fmt.Println("volume detached")

	if err := s.client.DeleteVolume(ctx, volume.ID); err != nil {
		return fmt.Errorf("delete volume: %w", err)
	}
	fmt.Println("volume deleted")
	s.volumeID = ""
	return nil
}

func (s *e2eState) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var errs []error
	if s.serverAID != "" {
		if _, err := s.client.DeleteServer(ctx, s.serverAID); err != nil && !strings.Contains(err.Error(), "server_not_found") {
			errs = append(errs, fmt.Errorf("delete server-a %s: %w", s.serverAID, err))
		}
		s.serverAID = ""
	}
	if s.serverBID != "" {
		if _, err := s.client.DeleteServer(ctx, s.serverBID); err != nil && !strings.Contains(err.Error(), "server_not_found") {
			errs = append(errs, fmt.Errorf("delete server-b %s: %w", s.serverBID, err))
		}
		s.serverBID = ""
	}
	if s.volumeID != "" {
		if err := s.client.DeleteVolume(ctx, s.volumeID); err != nil && !strings.Contains(err.Error(), "volume_not_found") {
			errs = append(errs, fmt.Errorf("delete volume %s: %w", s.volumeID, err))
		}
		s.volumeID = ""
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func mustProjectScope(projectID string) authn.Scope {
	return authn.Scope{
		Type:      authn.ScopeTypeProject,
		ProjectID: projectID,
	}
}

func mustServerRequest(name, imageID, flavor, networkID string) compute.CreateServerRequest {
	return compute.CreateServerRequest{
		Name:           name,
		ImageID:        imageID,
		Flavor:         flavor,
		Networks:       []string{networkID},
		TraitsRequired: []string{"general"},
	}
}

func pickCIDR(seed int64) string {
	second := 120 + int(seed%50)
	third := 10 + int((seed/1000)%200)
	return fmt.Sprintf("10.%d.%d.0/24", second, third)
}

func gatewayIP(cidr string) (string, error) {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("parse cidr %s: %w", cidr, err)
	}
	v4 := ip.To4()
	if v4 == nil {
		return "", fmt.Errorf("cidr %s is not IPv4", cidr)
	}
	v4[3]++
	return v4.String(), nil
}

func fixedIP(port networkkit.Port) (string, error) {
	if len(port.FixedIPs) == 0 {
		return "", errors.New("port has no fixed IP")
	}
	return port.FixedIPs[0].IPAddress, nil
}

func checkOVNPort(db string, port networkkit.Port, expectedIP string) error {
	lspName := strings.TrimPrefix(port.ID, "port_")
	ovs, err := openOVNNBClient(db)
	if err != nil {
		return err
	}
	defer ovs.Disconnect()
	defer ovs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lsp := &ovnLogicalSwitchPort{Name: lspName}
	if err := ovs.Get(ctx, lsp); err != nil {
		return err
	}
	if lsp.Up == nil || !*lsp.Up {
		return fmt.Errorf("logical switch port %s is not up", lspName)
	}
	hasIP := false
	for _, addr := range lsp.Addresses {
		if strings.Contains(addr, expectedIP) {
			hasIP = true
			break
		}
	}
	if !hasIP {
		return fmt.Errorf("logical switch port %s missing IP %s", lspName, expectedIP)
	}
	return nil
}

func openOVNNBClient(endpoint string) (libovsdbclient.Client, error) {
	clientDBModel, err := model.NewClientDBModel(ovnNBDBName, map[string]model.Model{
		"Logical_Switch_Port": &ovnLogicalSwitchPort{},
	})
	if err != nil {
		return nil, err
	}
	ovs, err := libovsdbclient.NewOVSDBClient(clientDBModel, libovsdbclient.WithEndpoint(endpoint))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ovs.Connect(ctx); err != nil {
		ovs.Close()
		return nil, err
	}
	if _, err := ovs.MonitorAll(ctx); err != nil {
		ovs.Disconnect()
		ovs.Close()
		return nil, err
	}
	return ovs, nil
}

func checkDomainInterface(serverID, mac string) error {
	conn, dom, err := openDomain(domainName(serverID))
	if err != nil {
		return err
	}
	defer dom.Free()
	defer conn.Close()

	cfg, err := readDomainConfig(dom)
	if err != nil {
		return err
	}
	for _, iface := range cfg.Devices.Interfaces {
		bridge := ""
		if iface.Source != nil && iface.Source.Bridge != nil {
			bridge = iface.Source.Bridge.Bridge
		}
		address := ""
		if iface.MAC != nil {
			address = iface.MAC.Address
		}
		if bridge == "br-int" && strings.EqualFold(address, mac) {
			return nil
		}
	}
	return fmt.Errorf("domain interface missing br-int/mac %s", mac)
}

func waitForDomainBlock(domain, devicePath string, wantPresent bool, timeout time.Duration) error {
	conn, dom, err := openDomain(domain)
	if err != nil {
		return err
	}
	defer dom.Free()
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cfg, err := readDomainConfig(dom)
		if err == nil {
			present := false
			for _, disk := range cfg.Devices.Disks {
				if disk.Source == nil {
					continue
				}
				if disk.Source.Block != nil && disk.Source.Block.Dev == devicePath {
					present = true
					break
				}
				if disk.Source.File != nil && disk.Source.File.File == devicePath {
					present = true
					break
				}
			}
			if present == wantPresent {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if wantPresent {
		return fmt.Errorf("device %s did not appear", devicePath)
	}
	return fmt.Errorf("device %s did not disappear", devicePath)
}

func openConsole(domain string) (*consoleSession, error) {
	conn, dom, err := openDomain(domain)
	if err != nil {
		return nil, err
	}
	stream, err := conn.NewStream(0)
	if err != nil {
		_ = dom.Free()
		_, _ = conn.Close()
		return nil, err
	}
	if err := dom.OpenConsole("", stream, 0); err != nil {
		_ = stream.Free()
		_ = dom.Free()
		_, _ = conn.Close()
		return nil, err
	}
	session := &consoleSession{
		conn:   conn,
		domain: dom,
		stream: stream,
	}
	go session.readLoop()
	return session, nil
}

func openDomain(name string) (*libvirt.Connect, *libvirt.Domain, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, nil, err
	}
	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		_, _ = conn.Close()
		return nil, nil, err
	}
	return conn, dom, nil
}

func readDomainConfig(dom *libvirt.Domain) (*libvirtxml.Domain, error) {
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return nil, err
	}
	var cfg libvirtxml.Domain
	if err := cfg.Unmarshal(xmlDesc); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *consoleSession) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.stream.Recv(buf)
		if n > 0 {
			s.mu.Lock()
			_, _ = s.buf.Write(buf[:n])
			if s.buf.Len() > 256*1024 {
				tail := append([]byte(nil), s.buf.Bytes()[s.buf.Len()-128*1024:]...)
				s.buf.Reset()
				_, _ = s.buf.Write(tail)
			}
			s.mu.Unlock()
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			return
		}
	}
}

func (s *consoleSession) Close() {
	if s.stream != nil {
		_ = s.stream.Abort()
		_ = s.stream.Free()
	}
	if s.domain != nil {
		_ = s.domain.Free()
	}
	if s.conn != nil {
		_, _ = s.conn.Close()
	}
}

func (s *consoleSession) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}

func (s *consoleSession) Snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *consoleSession) Send(input string) error {
	data := []byte(input)
	for len(data) > 0 {
		n, err := s.stream.Send(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func (s *consoleSession) WaitForAny(patterns []string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snapshot := s.Snapshot()
		for _, pattern := range patterns {
			if strings.Contains(snapshot, pattern) {
				return snapshot, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return s.Snapshot(), fmt.Errorf("timeout waiting for patterns: %s", strings.Join(patterns, ", "))
}

func loginCirros(console *consoleSession, timeout time.Duration) error {
	if _, err := console.WaitForAny([]string{"cirros login:", "login:"}, timeout); err != nil {
		return err
	}
	console.Clear()
	if err := console.Send("cirros\n"); err != nil {
		return err
	}
	if _, err := console.WaitForAny([]string{"Password:"}, 20*time.Second); err != nil {
		return err
	}
	console.Clear()
	if err := console.Send("gocubsgo\n"); err != nil {
		return err
	}
	if _, err := console.WaitForAny([]string{"\n$ ", "\n# "}, 20*time.Second); err != nil {
		return err
	}
	return nil
}

func runGuestCommand(console *consoleSession, command string, timeout time.Duration) (string, error) {
	marker := fmt.Sprintf("__ORION_E2E_%d__", time.Now().UnixNano())
	console.Clear()
	if err := console.Send(command + "\necho " + marker + "\n"); err != nil {
		return "", err
	}
	if _, err := console.WaitForAny([]string{marker}, timeout); err != nil {
		return "", err
	}
	output := console.Snapshot()
	if idx := strings.Index(output, marker); idx >= 0 {
		output = output[:idx]
	}
	return strings.TrimSpace(output), nil
}

func waitForGuestPing(console *consoleSession, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastOutput string
	for time.Now().Before(deadline) {
		output, err := runGuestCommand(console, fmt.Sprintf("ping -c 1 -W 1 %s", target), 10*time.Second)
		if err == nil && strings.Contains(output, "0% packet loss") {
			return nil
		}
		if err == nil {
			lastOutput = output
		} else {
			lastOutput = err.Error()
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("target %s did not answer ping; last output: %s", target, strings.TrimSpace(lastOutput))
}

func section(title string) {
	fmt.Printf("\n== %s ==\n", title)
}

func printHealth(items []orioncli.HealthResult) {
	for _, item := range items {
		if item.Error == "" {
			fmt.Printf("%-18s %-4s %s\n", item.Name, item.Status, item.URL)
			continue
		}
		fmt.Printf("%-18s %-4s %s (%s)\n", item.Name, item.Status, item.URL, item.Error)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func firstOrEmpty(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func lastNonEmptyLine(input string) string {
	lines := strings.Split(strings.TrimSpace(input), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" && !strings.HasPrefix(line, "$") {
			return line
		}
	}
	return ""
}

func isVolumeBackendUnavailable(err error) bool {
	message := err.Error()
	return strings.Contains(message, "volume_backend_unavailable") ||
		strings.Contains(message, "ORION_VOLUME_LVM_VG is required") ||
		strings.Contains(message, "Permission denied")
}

func domainName(serverID string) string {
	return "orion-" + serverID
}
