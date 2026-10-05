package cli

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/term"
	"os"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
)

// NewClientFromEnv constructs a Client from environment variables.
func NewClientFromEnv() *Client {
	cfg := Config{
		APIURL:              envOrDefault("ORION_API_URL", "http://127.0.0.1:8080"),
		IdentityURL:         envOrDefault("ORION_IDENTITY_URL", "http://127.0.0.1:8081"),
		PlacementURL:        envOrDefault("ORION_PLACEMENT_URL", "http://127.0.0.1:8082"),
		ImageURL:            envOrDefault("ORION_IMAGE_URL", "http://127.0.0.1:8085"),
		NetworkURL:          envOrDefault("ORION_NETWORK_URL", "http://127.0.0.1:8086"),
		NodeAgentURL:        envOrDefault("ORION_NODE_AGENT_URL", "http://127.0.0.1:8084"),
		NetworkHostAgentURL: envOrDefault("ORION_NETWORK_HOST_AGENT_URL", "http://127.0.0.1:8087"),
		VolumeURL:           envOrDefault("ORION_VOLUME_URL", "http://127.0.0.1:8087"),
		VolumeHostAgentURL:  envOrDefault("ORION_VOLUME_HOST_AGENT_URL", "http://127.0.0.1:8088"),
		Token:               envOrDefault("ORION_TOKEN", ""),
	}

	// If we have saved credentials, prefer them over environment token
	if creds, err := loadCredentials(); err == nil {
		if creds.Token != "" {
			cfg.Token = creds.Token
		}
		if creds.ProjectID != "" {
			cfg.DefaultProject = creds.ProjectID
		}
	}

	return NewClient(cfg)
}

// CLI is the root Kong command structure for the orion CLI.
type CLI struct {
	Auth    authGroup    `cmd:"" help:"Authentication operations."`
	Token   tokenGroup   `cmd:"" help:"Token operations."`
	Health  healthGroup  `cmd:"" help:"Health operations."`
	Image   imageGroup   `cmd:"" help:"Image operations."`
	Host    hostGroup    `cmd:"" help:"Host operations."`
	Network networkGroup `cmd:"" help:"Network operations."`
	Subnet  subnetGroup  `cmd:"" help:"Subnet operations."`
	Port    portGroup    `cmd:"" help:"Port operations."`
	Server  serverGroup  `cmd:"" help:"Server operations."`
	Volume  volumeGroup  `cmd:"" help:"Volume operations."`
	Task    taskGroup    `cmd:"" help:"Task operations."`
	Plugin  pluginGroup  `cmd:"" help:"Plugin lifecycle operations."`
}

type authGroup struct {
	Login authLoginCmd `cmd:"" help:"Interactive login and project selection."`
}

type authLoginCmd struct {
	Username string `default:"admin" help:"Username."`
}

func (c *authLoginCmd) Run(client *Client) error {
	// prompt for username
	username := c.Username
	fmt.Printf("Username [%s]: ", username)
	var input string
	if _, err := fmt.Fscanln(os.Stdin, &input); err == nil {
		if strings.TrimSpace(input) != "" {
			username = strings.TrimSpace(input)
		}
	}

	fmt.Print("Password: ")
	passBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}
	password := string(passBytes)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	projects, err := client.Login(ctx, username, password)
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("no projects available for this user")
		return nil
	}
	for i, p := range projects {
		fmt.Printf("%d) %s (%s)\n", i+1, p.Name, p.ID)
	}
	var sel int
	fmt.Printf("Select project number [1]: ")
	if _, err := fmt.Fscanln(os.Stdin, &sel); err != nil || sel < 1 || sel > len(projects) {
		sel = 1
	}
	chosen := projects[sel-1]

	// Issue token scoped to chosen project
	token, err := client.IssueToken(ctx, AuthenticateRequest{
		Username: username,
		Password: password,
		Scope:    authn.Scope{Type: authn.ScopeTypeProject, ProjectID: chosen.ID},
	})
	if err != nil {
		return err
	}

	// Save credentials
	if err := saveCredentials(savedCredentials{Token: token.Value, ProjectID: chosen.ID}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save credentials: %v\n", err)
	} else {
		fmt.Println("Saved credentials to config.")
	}

	fmt.Println("Login successful. Token saved.")
	return nil
}

// outputFlags is embedded in commands that support --output and --field.
type outputFlags struct {
	Output string `short:"o" default:"human" enum:"human,json" help:"Output format (human|json)."`
	Field  string `help:"Field path to extract and print."`
}

// listOutputFlags is embedded in list commands that also support --wide.
type listOutputFlags struct {
	outputFlags
	Wide bool `short:"w" help:"Show more columns."`
}

// tokenFlag is embedded in commands that require a bearer token.
type tokenFlag struct {
	Token string `env:"ORION_TOKEN" help:"Bearer token."`
}

func (t *tokenFlag) requireToken() error {
	if t.Token == "" {
		return errors.New("missing --token or ORION_TOKEN")
	}
	return nil
}

// ── token ──────────────────────────────────────────────────────────────────

type tokenGroup struct {
	Issue    tokenIssueCmd    `cmd:"" help:"Issue a new token."`
	Validate tokenValidateCmd `cmd:"" help:"Validate an existing token."`
}

type tokenIssueCmd struct {
	Username  string `default:"admin" help:"Username."`
	Password  string `default:"orion-admin" help:"Password."`
	Scope     string `default:"project" enum:"project,system" help:"Token scope type."`
	Project   string `default:"proj_admin" help:"Project ID (for project scope)."`
	ValueOnly bool   `help:"Print only the raw token value."`
	outputFlags
}

func (c *tokenIssueCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	scope := authn.Scope{Type: authn.ScopeType(c.Scope)}
	if scope.Type == authn.ScopeTypeProject {
		scope.ProjectID = c.Project
	}
	token, err := client.IssueToken(ctx, AuthenticateRequest{
		Username: c.Username,
		Password: c.Password,
		Scope:    scope,
	})
	if err != nil {
		return err
	}
	if c.ValueOnly {
		fmt.Println(token.Value)
		return nil
	}
	return renderSingle(c.Output, c.Field, "token", token, func() { printTokenHuman(token) })
}

type tokenValidateCmd struct {
	TokenValue string `name:"token" env:"ORION_TOKEN" help:"Token value to validate."`
	outputFlags
}

func (c *tokenValidateCmd) Run(client *Client) error {
	if c.TokenValue == "" {
		return errors.New("missing --token or ORION_TOKEN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	token, err := client.ValidateToken(ctx, c.TokenValue)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "token", token, func() { printTokenHuman(token) })
}

// ── health ─────────────────────────────────────────────────────────────────

type healthGroup struct {
	Check healthCheckCmd `cmd:"" help:"Check health of all services."`
}

type healthCheckCmd struct {
	outputFlags
}

func (c *healthCheckCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := client.HealthChecks(ctx)
	if c.Output == "json" {
		return printJSON(map[string]any{"checks": results})
	}
	printHealthTable(results)
	return nil
}

// ── image ──────────────────────────────────────────────────────────────────

type imageGroup struct {
	List imageListCmd `cmd:"" help:"List images."`
	Get  imageGetCmd  `cmd:"" help:"Get an image."`
}

type imageListCmd struct {
	listOutputFlags
}

func (c *imageListCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	items, err := client.ListImages(ctx)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(map[string]any{"images": items})
	}
	if c.Field != "" {
		return printFieldList(items, c.Field)
	}
	printImagesTable(items, c.Wide)
	return nil
}

type imageGetCmd struct {
	ID string `arg:"" help:"Image ID."`
	outputFlags
}

func (c *imageGetCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	item, err := client.GetImage(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "image", item, func() { printImageHuman(item) })
}

// ── host ───────────────────────────────────────────────────────────────────

type hostGroup struct {
	List    hostListCmd    `cmd:"" help:"List hosts."`
	Get     hostGetCmd     `cmd:"" help:"Get a host."`
	Enable  hostEnableCmd  `cmd:"" help:"Enable a host for scheduling."`
	Disable hostDisableCmd `cmd:"" help:"Disable a host for scheduling."`
	Drain   hostDrainCmd   `cmd:"" help:"Drain a host from new scheduling."`
	Undrain hostUndrainCmd `cmd:"" help:"Remove drain from a host."`
}

type hostListCmd struct {
	listOutputFlags
}

func (c *hostListCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	items, err := client.ListHosts(ctx)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(map[string]any{"hosts": items})
	}
	if c.Field != "" {
		return printFieldList(items, c.Field)
	}
	printHostsTable(items, c.Wide)
	return nil
}

type hostGetCmd struct {
	ID string `arg:"" help:"Host ID."`
	outputFlags
}

func (c *hostGetCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	item, err := client.GetHost(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "host", item, func() { printHostHuman(item) })
}

type hostEnableCmd struct {
	ID string `arg:"" help:"Host ID."`
	outputFlags
}

func (c *hostEnableCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	item, err := client.EnableHost(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "host", item, func() { printHostHuman(item) })
}

type hostDisableCmd struct {
	ID string `arg:"" help:"Host ID."`
	outputFlags
}

func (c *hostDisableCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	item, err := client.DisableHost(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "host", item, func() { printHostHuman(item) })
}

type hostDrainCmd struct {
	ID string `arg:"" help:"Host ID."`
	outputFlags
}

func (c *hostDrainCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	item, err := client.DrainHost(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "host", item, func() { printHostHuman(item) })
}

type hostUndrainCmd struct {
	ID string `arg:"" help:"Host ID."`
	outputFlags
}

func (c *hostUndrainCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	item, err := client.UndrainHost(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "host", item, func() { printHostHuman(item) })
}

// ── network ────────────────────────────────────────────────────────────────

type networkGroup struct {
	List   networkListCmd   `cmd:"" help:"List networks."`
	Get    networkGetCmd    `cmd:"" help:"Get a network."`
	Create networkCreateCmd `cmd:"" help:"Create a network."`
}

type networkListCmd struct {
	listOutputFlags
}

func (c *networkListCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := client.ListNetworks(ctx)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(map[string]any{"networks": items})
	}
	if c.Field != "" {
		return printFieldList(items, c.Field)
	}
	printNetworksTable(items, c.Wide)
	return nil
}

type networkGetCmd struct {
	ID string `arg:"" help:"Network ID."`
	outputFlags
}

func (c *networkGetCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	item, err := client.GetNetwork(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "network", item, func() { printNetworkHuman(item) })
}

type networkCreateCmd struct {
	Project string `help:"Project ID."`
	Name    string `required:"" help:"Network name."`
	outputFlags
}

func (c *networkCreateCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	project := c.Project
	if project == "" {
		project = client.cfg.DefaultProject
	}
	if project == "" {
		project = "proj_admin"
	}

	item, err := client.CreateNetwork(ctx, networkkit.CreateNetworkRequest{
		ProjectID: project,
		Name:      c.Name,
	})
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "network", item, func() { printNetworkHuman(item) })
}

// ── subnet ─────────────────────────────────────────────────────────────────

type subnetGroup struct {
	List   subnetListCmd   `cmd:"" help:"List subnets."`
	Get    subnetGetCmd    `cmd:"" help:"Get a subnet."`
	Create subnetCreateCmd `cmd:"" help:"Create a subnet."`
}

type subnetListCmd struct {
	listOutputFlags
}

func (c *subnetListCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := client.ListSubnets(ctx)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(map[string]any{"subnets": items})
	}
	if c.Field != "" {
		return printFieldList(items, c.Field)
	}
	printSubnetsTable(items, c.Wide)
	return nil
}

type subnetGetCmd struct {
	ID string `arg:"" help:"Subnet ID."`
	outputFlags
}

func (c *subnetGetCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	item, err := client.GetSubnet(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "subnet", item, func() { printSubnetHuman(item) })
}

type subnetCreateCmd struct {
	Project    string `default:"proj_admin" help:"Project ID."`
	Network    string `required:"" help:"Network ID."`
	Name       string `required:"" help:"Subnet name."`
	CIDR       string `required:"" help:"CIDR block (e.g. 10.0.0.0/24)."`
	Gateway    string `help:"Gateway IP address."`
	EnableDHCP bool   `default:"true" help:"Enable DHCP on this subnet."`
	outputFlags
}

func (c *subnetCreateCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	item, err := client.CreateSubnet(ctx, networkkit.CreateSubnetRequest{
		ProjectID:  c.Project,
		NetworkID:  c.Network,
		Name:       c.Name,
		CIDR:       c.CIDR,
		GatewayIP:  c.Gateway,
		EnableDHCP: c.EnableDHCP,
	})
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "subnet", item, func() { printSubnetHuman(item) })
}

// ── port ───────────────────────────────────────────────────────────────────

type portGroup struct {
	List   portListCmd   `cmd:"" help:"List ports."`
	Get    portGetCmd    `cmd:"" help:"Get a port."`
	Create portCreateCmd `cmd:"" help:"Create a port."`
}

type portListCmd struct {
	listOutputFlags
}

func (c *portListCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := client.ListPorts(ctx)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(map[string]any{"ports": items})
	}
	if c.Field != "" {
		return printFieldList(items, c.Field)
	}
	printPortsTable(items, c.Wide)
	return nil
}

type portGetCmd struct {
	ID string `arg:"" help:"Port ID."`
	outputFlags
}

func (c *portGetCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	item, err := client.GetPort(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "port", item, func() { printPortHuman(item) })
}

type portCreateCmd struct {
	Project     string `default:"proj_admin" help:"Project ID."`
	Network     string `required:"" help:"Network ID."`
	DeviceID    string `name:"device-id" help:"Device ID."`
	DeviceOwner string `name:"device-owner" help:"Device owner."`
	BindingHost string `name:"binding-host" help:"Binding host ID."`
	outputFlags
}

func (c *portCreateCmd) Run(client *Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	item, err := client.CreatePort(ctx, networkkit.CreatePortRequest{
		ProjectID:     c.Project,
		NetworkID:     c.Network,
		DeviceID:      c.DeviceID,
		DeviceOwner:   c.DeviceOwner,
		BindingHostID: c.BindingHost,
	})
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "port", item, func() { printPortHuman(item) })
}

// ── server ─────────────────────────────────────────────────────────────────

type serverGroup struct {
	List         serverListCmd         `cmd:"" help:"List servers."`
	Get          serverGetCmd          `cmd:"" help:"Get a server."`
	Create       serverCreateCmd       `cmd:"" help:"Create a server."`
	Delete       serverDeleteCmd       `cmd:"" help:"Delete a server."`
	AttachVolume serverAttachVolumeCmd `cmd:"attach-volume" help:"Attach a volume to a server."`
	DetachVolume serverDetachVolumeCmd `cmd:"detach-volume" help:"Detach a volume from a server."`
}

type serverListCmd struct {
	tokenFlag
	listOutputFlags
}

func (c *serverListCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	items, err := client.ListServers(ctx)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(map[string]any{"servers": items})
	}
	if c.Field != "" {
		return printFieldList(items, c.Field)
	}
	printServersTable(items, c.Wide)
	return nil
}

type serverGetCmd struct {
	ID string `arg:"" help:"Server ID."`
	tokenFlag
	outputFlags
}

func (c *serverGetCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	item, err := client.GetServer(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "server", item, func() { printServerHuman(item) })
}

type serverCreateCmd struct {
	Name     string `required:"" help:"Server name."`
	Image    string `required:"" help:"Image ID."`
	Flavor   string `default:"tiny" help:"Flavor name."`
	Networks string `help:"Comma-separated network IDs."`
	Traits   string `default:"general" help:"Comma-separated required traits."`
	tokenFlag
	outputFlags
}

func (c *serverCreateCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	response, err := client.CreateServer(ctx, compute.CreateServerRequest{
		Name:           c.Name,
		ImageID:        c.Image,
		Flavor:         c.Flavor,
		Networks:       splitComma(c.Networks),
		TraitsRequired: splitComma(c.Traits),
	})
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(response)
	}
	if c.Field != "" {
		return printField(response, c.Field)
	}
	printServerHuman(response.Server)
	fmt.Println()
	printTaskHuman(response.Task)
	return nil
}

type serverDeleteCmd struct {
	ID string `arg:"" help:"Server ID."`
	tokenFlag
	outputFlags
}

func (c *serverDeleteCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	resourceTask, err := client.DeleteServer(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "task", resourceTask, func() { printTaskHuman(resourceTask) })
}

type serverAttachVolumeCmd struct {
	ID     string `arg:"" help:"Server ID."`
	Volume string `required:"" help:"Volume ID."`
	tokenFlag
	outputFlags
}

func (c *serverAttachVolumeCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	response, err := client.AttachVolume(ctx, c.ID, c.Volume)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(response)
	}
	if c.Field != "" {
		return printField(response, c.Field)
	}
	printServerHuman(response.Server)
	fmt.Println()
	printTaskHuman(response.Task)
	return nil
}

type serverDetachVolumeCmd struct {
	ID     string `arg:"" help:"Server ID."`
	Volume string `required:"" help:"Volume ID."`
	tokenFlag
	outputFlags
}

func (c *serverDetachVolumeCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	response, err := client.DetachVolume(ctx, c.ID, c.Volume)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(response)
	}
	if c.Field != "" {
		return printField(response, c.Field)
	}
	printServerHuman(response.Server)
	fmt.Println()
	printTaskHuman(response.Task)
	return nil
}

// ── volume ─────────────────────────────────────────────────────────────────

type volumeGroup struct {
	List   volumeListCmd   `cmd:"" help:"List volumes."`
	Get    volumeGetCmd    `cmd:"" help:"Get a volume."`
	Create volumeCreateCmd `cmd:"" help:"Create a volume."`
	Delete volumeDeleteCmd `cmd:"" help:"Delete a volume."`
}

type volumeListCmd struct {
	tokenFlag
	listOutputFlags
}

func (c *volumeListCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	items, err := client.ListVolumes(ctx)
	if err != nil {
		return err
	}
	if c.Output == "json" {
		return printJSON(map[string]any{"volumes": items})
	}
	if c.Field != "" {
		return printFieldList(items, c.Field)
	}
	printVolumesTable(items, c.Wide)
	return nil
}

type volumeGetCmd struct {
	ID string `arg:"" help:"Volume ID."`
	tokenFlag
	outputFlags
}

func (c *volumeGetCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	item, err := client.GetVolume(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "volume", item, func() { printVolumeHuman(item) })
}

type volumeCreateCmd struct {
	Name   string `required:"" help:"Volume name."`
	SizeGB int    `name:"size-gb" default:"1" help:"Volume size in GB."`
	tokenFlag
	outputFlags
}

func (c *volumeCreateCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	item, err := client.CreateVolume(ctx, volumekit.CreateVolumeRequest{
		Name:   c.Name,
		SizeGB: c.SizeGB,
	})
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "volume", item, func() { printVolumeHuman(item) })
}

type volumeDeleteCmd struct {
	ID string `arg:"" help:"Volume ID."`
	tokenFlag
}

func (c *volumeDeleteCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	return client.DeleteVolume(ctx, c.ID)
}

// ── task ───────────────────────────────────────────────────────────────────

type taskGroup struct {
	Get taskGetCmd `cmd:"" help:"Get a task."`
}

type taskGetCmd struct {
	ID string `arg:"" help:"Task ID."`
	tokenFlag
	outputFlags
}

func (c *taskGetCmd) Run(client *Client) error {
	if err := c.requireToken(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client.cfg.Token = c.Token
	item, err := client.GetTask(ctx, c.ID)
	if err != nil {
		return err
	}
	return renderSingle(c.Output, c.Field, "task", item, func() { printTaskHuman(item) })
}

// ── helpers ────────────────────────────────────────────────────────────────

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitComma(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func renderSingle(output, field, envelopeKey string, item any, human func()) error {
	if output == "json" {
		return printJSON(map[string]any{envelopeKey: item})
	}
	if field != "" {
		return printField(item, field)
	}
	human()
	return nil
}
