package placementclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/retry"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *Client) SelectHost(ctx context.Context, req ports.SelectHostRequest) (ports.HostSelection, error) {
	payload := map[string]any{
		"server_id":                 req.ServerID,
		"cell_id":                   req.CellID,
		"vcpus":                     req.VCPUs,
		"memory_mb":                 req.MemoryMB,
		"disk_gb":                   req.DiskGB,
		"traits_required":           req.TraitsRequired,
		"require_node_agent":        req.RequireNodeAgent,
		"require_volume_host_agent": req.RequireVolumeHostAgent,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ports.HostSelection{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/selections/hosts", bytes.NewReader(body))
	if err != nil {
		return ports.HostSelection{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), body, httpReq.Header)
	if err != nil {
		return ports.HostSelection{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return ports.HostSelection{}, ErrNoValidHost
	}

	if resp.StatusCode != http.StatusOK {
		return ports.HostSelection{}, fmt.Errorf("placement select failed with status %d", resp.StatusCode)
	}

	var response struct {
		Selection struct {
			CellID string `json:"cell_id"`
			HostID string `json:"host_id"`
		} `json:"selection"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return ports.HostSelection{}, err
	}

	return ports.HostSelection{
		CellID: response.Selection.CellID,
		HostID: response.Selection.HostID,
	}, nil
}

func (c *Client) ReleaseHost(ctx context.Context, req ports.ReleaseHostRequest) error {
	body, err := json.Marshal(map[string]any{
		"server_id": req.ServerID,
		"host_id":   req.HostID,
		"vcpus":     req.VCPUs,
		"memory_mb": req.MemoryMB,
		"disk_gb":   req.DiskGB,
	})
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/allocations/release", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), body, httpReq.Header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("placement release failed with status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) GetHost(ctx context.Context, hostID string) (ports.HostRecord, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/hosts/"+hostID, nil)
	if err != nil {
		return ports.HostRecord{}, err
	}
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), nil, httpReq.Header)
	if err != nil {
		return ports.HostRecord{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ports.HostRecord{}, ErrHostNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ports.HostRecord{}, fmt.Errorf("placement get host failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var response struct {
		Host struct {
			HostID              string `json:"host_id"`
			NodeAgentURL        string `json:"node_agent_url"`
			NodeAgentGRPCURL    string `json:"node_agent_grpc_url"`
			VolumeHostAgentURL  string `json:"volume_host_agent_url"`
			NetworkHostAgentURL string `json:"network_host_agent_url"`
		} `json:"host"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return ports.HostRecord{}, err
	}

	return ports.HostRecord{
		HostID:              response.Host.HostID,
		NodeAgentURL:        response.Host.NodeAgentURL,
		NodeAgentGRPCURL:    response.Host.NodeAgentGRPCURL,
		VolumeHostAgentURL:  response.Host.VolumeHostAgentURL,
		NetworkHostAgentURL: response.Host.NetworkHostAgentURL,
	}, nil
}

var ErrNoValidHost = fmt.Errorf("no valid host")
var ErrHostNotFound = fmt.Errorf("host not found")
