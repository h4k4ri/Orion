package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/image"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/libs/go/kit/task"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
)

type Config struct {
	APIURL              string
	IdentityURL         string
	PlacementURL        string
	ImageURL            string
	NetworkURL          string
	NodeAgentURL        string
	NetworkHostAgentURL string
	VolumeURL           string
	VolumeHostAgentURL  string
	Token               string
	DefaultProject      string
}

type Client struct {
	cfg    Config
	client *http.Client
}

type AuthenticateRequest struct {
	Username string      `json:"username"`
	Password string      `json:"password"`
	Scope    authn.Scope `json:"scope"`
}

type Host struct {
	HostID     string        `json:"host_id"`
	CellID     string        `json:"cell_id"`
	Group      string        `json:"group"`
	Enabled    bool          `json:"enabled"`
	Drained    bool          `json:"drained"`
	Traits     []string      `json:"traits"`
	Inventory  HostInventory `json:"inventory"`
	Generation int64         `json:"generation"`
}

type HostInventory struct {
	VCPUsTotal        int             `json:"vcpus_total"`
	VCPUsAllocated    int             `json:"vcpus_allocated"`
	MemoryMBTotal     int             `json:"memory_mb_total"`
	MemoryAllocatedMB int             `json:"memory_mb_allocated"`
	DiskGBTotal       int             `json:"disk_gb_total"`
	DiskAllocatedGB   int             `json:"disk_gb_allocated"`
	NUMA              []HostNUMANode  `json:"numa"`
	GPUs              []HostGPUDevice `json:"gpus"`
}

type HostNUMANode struct {
	ID                int   `json:"id"`
	VCPUs             []int `json:"vcpus"`
	MemoryMBTotal     int   `json:"memory_mb_total"`
	MemoryAllocatedMB int   `json:"memory_mb_allocated"`
}

type HostGPUDevice struct {
	ID          string   `json:"id"`
	Vendor      string   `json:"vendor"`
	Model       string   `json:"model"`
	MemoryMB    int      `json:"memory_mb"`
	Traits      []string `json:"traits"`
	AllocatedTo string   `json:"allocated_to,omitempty"`
}

type HealthResult struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) IssueToken(ctx context.Context, req AuthenticateRequest) (authn.Token, error) {
	var response struct {
		Token authn.Token `json:"token"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.IdentityURL + "/v1/auth/tokens",
		Body:   req,
	}, &response)
	return response.Token, err
}

func (c *Client) ValidateToken(ctx context.Context, tokenValue string) (authn.Token, error) {
	var response struct {
		Token authn.Token `json:"token"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.IdentityURL + "/v1/auth/tokens",
		Headers: map[string]string{
			"X-Subject-Token": tokenValue,
		},
	}, &response)
	return response.Token, err
}

// Login calls identity /v1/auth/login to validate credentials and
// retrieve the list of projects attached to the user.
func (c *Client) Login(ctx context.Context, username, password string) ([]struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}, error) {
	var response struct {
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.IdentityURL + "/v1/auth/login",
		Body:   map[string]string{"username": username, "password": password},
	}, &response)
	return response.Projects, err
}

func (c *Client) HealthChecks(ctx context.Context) []HealthResult {
	targets := []struct {
		Name string
		URL  string
	}{
		{Name: "api", URL: c.cfg.APIURL + "/healthz"},
		{Name: "identity", URL: c.cfg.IdentityURL + "/healthz"},
		{Name: "placement", URL: c.cfg.PlacementURL + "/healthz"},
		{Name: "image", URL: c.cfg.ImageURL + "/healthz"},
		{Name: "network", URL: c.cfg.NetworkURL + "/healthz"},
		{Name: "node-agent", URL: c.cfg.NodeAgentURL + "/healthz"},
		{Name: "network-host-agent", URL: c.cfg.NetworkHostAgentURL + "/healthz"},
		{Name: "volume", URL: c.cfg.VolumeURL + "/healthz"},
		{Name: "volume-host-agent", URL: c.cfg.VolumeHostAgentURL + "/healthz"},
	}

	results := make([]HealthResult, 0, len(targets))
	for _, target := range targets {
		result := HealthResult{Name: target.Name, URL: target.URL, Status: "down"}
		var response struct {
			Status string `json:"status"`
		}
		if err := c.doJSON(ctx, requestSpec{
			Method: http.MethodGet,
			URL:    target.URL,
		}, &response); err != nil {
			result.Error = err.Error()
		} else {
			result.Status = response.Status
		}
		results = append(results, result)
	}

	return results
}

func (c *Client) ListImages(ctx context.Context) ([]image.Image, error) {
	var response struct {
		Images []image.Image `json:"images"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.ImageURL + "/v1/images",
	}, &response)
	return response.Images, err
}

func (c *Client) GetImage(ctx context.Context, id string) (image.Image, error) {
	var response struct {
		Image image.Image `json:"image"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.ImageURL + "/v1/images/" + id,
	}, &response)
	return response.Image, err
}

func (c *Client) ListHosts(ctx context.Context) ([]Host, error) {
	var response struct {
		Hosts []Host `json:"hosts"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.PlacementURL + "/v1/hosts",
	}, &response)
	return response.Hosts, err
}

func (c *Client) GetHost(ctx context.Context, id string) (Host, error) {
	var response struct {
		Host Host `json:"host"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.PlacementURL + "/v1/hosts/" + id,
	}, &response)
	return response.Host, err
}

func (c *Client) updateHostState(ctx context.Context, id, action string) (Host, error) {
	var response struct {
		Host Host `json:"host"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.PlacementURL + "/v1/hosts/" + id + "/" + action,
	}, &response)
	return response.Host, err
}

func (c *Client) EnableHost(ctx context.Context, id string) (Host, error) {
	return c.updateHostState(ctx, id, "enable")
}

func (c *Client) DisableHost(ctx context.Context, id string) (Host, error) {
	return c.updateHostState(ctx, id, "disable")
}

func (c *Client) DrainHost(ctx context.Context, id string) (Host, error) {
	return c.updateHostState(ctx, id, "drain")
}

func (c *Client) UndrainHost(ctx context.Context, id string) (Host, error) {
	return c.updateHostState(ctx, id, "undrain")
}

func (c *Client) ListNetworks(ctx context.Context) ([]networkkit.Network, error) {
	var response struct {
		Networks []networkkit.Network `json:"networks"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.NetworkURL + "/v1/networks",
	}, &response)
	return response.Networks, err
}

func (c *Client) GetNetwork(ctx context.Context, id string) (networkkit.Network, error) {
	var response struct {
		Network networkkit.Network `json:"network"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.NetworkURL + "/v1/networks/" + id,
	}, &response)
	return response.Network, err
}

func (c *Client) CreateNetwork(ctx context.Context, req networkkit.CreateNetworkRequest) (networkkit.Network, error) {
	var response struct {
		Network networkkit.Network `json:"network"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.NetworkURL + "/v1/networks",
		Body:   req,
	}, &response)
	return response.Network, err
}

func (c *Client) ListSubnets(ctx context.Context) ([]networkkit.Subnet, error) {
	var response struct {
		Subnets []networkkit.Subnet `json:"subnets"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.NetworkURL + "/v1/subnets",
	}, &response)
	return response.Subnets, err
}

func (c *Client) GetSubnet(ctx context.Context, id string) (networkkit.Subnet, error) {
	var response struct {
		Subnet networkkit.Subnet `json:"subnet"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.NetworkURL + "/v1/subnets/" + id,
	}, &response)
	return response.Subnet, err
}

func (c *Client) CreateSubnet(ctx context.Context, req networkkit.CreateSubnetRequest) (networkkit.Subnet, error) {
	var response struct {
		Subnet networkkit.Subnet `json:"subnet"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.NetworkURL + "/v1/subnets",
		Body:   req,
	}, &response)
	return response.Subnet, err
}

func (c *Client) ListPorts(ctx context.Context) ([]networkkit.Port, error) {
	var response struct {
		Ports []networkkit.Port `json:"ports"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.NetworkURL + "/v1/ports",
	}, &response)
	return response.Ports, err
}

func (c *Client) GetPort(ctx context.Context, id string) (networkkit.Port, error) {
	var response struct {
		Port networkkit.Port `json:"port"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.NetworkURL + "/v1/ports/" + id,
	}, &response)
	return response.Port, err
}

func (c *Client) CreatePort(ctx context.Context, req networkkit.CreatePortRequest) (networkkit.Port, error) {
	var response struct {
		Port networkkit.Port `json:"port"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.NetworkURL + "/v1/ports",
		Body:   req,
	}, &response)
	return response.Port, err
}

func (c *Client) GetServer(ctx context.Context, id string) (compute.Server, error) {
	var response struct {
		Server compute.Server `json:"server"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.APIURL + "/v1/servers/" + id,
		Token:  c.cfg.Token,
	}, &response)
	return response.Server, err
}

func (c *Client) ListServers(ctx context.Context) ([]compute.Server, error) {
	var response struct {
		Servers []compute.Server `json:"servers"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.APIURL + "/v1/servers",
		Token:  c.cfg.Token,
	}, &response)
	return response.Servers, err
}

func (c *Client) CreateServer(ctx context.Context, req compute.CreateServerRequest) (compute.CreateServerResponse, error) {
	var response compute.CreateServerResponse
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.APIURL + "/v1/servers",
		Token:  c.cfg.Token,
		Body:   req,
	}, &response)
	return response, err
}

func (c *Client) DeleteServer(ctx context.Context, id string) (task.Task, error) {
	var response struct {
		Task task.Task `json:"task"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodDelete,
		URL:    c.cfg.APIURL + "/v1/servers/" + id,
		Token:  c.cfg.Token,
	}, &response)
	return response.Task, err
}

func (c *Client) AttachVolume(ctx context.Context, serverID, volumeID string) (compute.CreateServerResponse, error) {
	var response compute.CreateServerResponse
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.APIURL + "/v1/servers/" + serverID + "/attach-volume",
		Token:  c.cfg.Token,
		Body: compute.AttachVolumeRequest{
			VolumeID: volumeID,
		},
	}, &response)
	return response, err
}

func (c *Client) DetachVolume(ctx context.Context, serverID, volumeID string) (compute.CreateServerResponse, error) {
	var response compute.CreateServerResponse
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.APIURL + "/v1/servers/" + serverID + "/detach-volume",
		Token:  c.cfg.Token,
		Body: compute.AttachVolumeRequest{
			VolumeID: volumeID,
		},
	}, &response)
	return response, err
}

func (c *Client) GetTask(ctx context.Context, id string) (task.Task, error) {
	var response struct {
		Task task.Task `json:"task"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.APIURL + "/v1/tasks/" + id,
		Token:  c.cfg.Token,
	}, &response)
	return response.Task, err
}

func (c *Client) ListVolumes(ctx context.Context) ([]volumekit.Volume, error) {
	var response struct {
		Volumes []volumekit.Volume `json:"volumes"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.APIURL + "/v1/volumes",
		Token:  c.cfg.Token,
	}, &response)
	return response.Volumes, err
}

func (c *Client) GetVolume(ctx context.Context, id string) (volumekit.Volume, error) {
	var response struct {
		Volume volumekit.Volume `json:"volume"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodGet,
		URL:    c.cfg.APIURL + "/v1/volumes/" + id,
		Token:  c.cfg.Token,
	}, &response)
	return response.Volume, err
}

func (c *Client) CreateVolume(ctx context.Context, req volumekit.CreateVolumeRequest) (volumekit.Volume, error) {
	var response struct {
		Volume volumekit.Volume `json:"volume"`
	}
	err := c.doJSON(ctx, requestSpec{
		Method: http.MethodPost,
		URL:    c.cfg.APIURL + "/v1/volumes",
		Token:  c.cfg.Token,
		Body:   req,
	}, &response)
	return response.Volume, err
}

func (c *Client) DeleteVolume(ctx context.Context, id string) error {
	return c.doJSON(ctx, requestSpec{
		Method: http.MethodDelete,
		URL:    c.cfg.APIURL + "/v1/volumes/" + id,
		Token:  c.cfg.Token,
	}, nil)
}

type requestSpec struct {
	Method  string
	URL     string
	Token   string
	Headers map[string]string
	Body    any
}

func (c *Client) doJSON(ctx context.Context, spec requestSpec, out any) error {
	var body io.Reader
	if spec.Body != nil {
		payload, err := json.Marshal(spec.Body)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, spec.Method, spec.URL, body)
	if err != nil {
		return err
	}
	if spec.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if spec.Token != "" {
		req.Header.Set("Authorization", "Bearer "+spec.Token)
	}
	for key, value := range spec.Headers {
		req.Header.Set(key, value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		var envelope apierror.Envelope
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
			return fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
		}
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("%s", message)
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
