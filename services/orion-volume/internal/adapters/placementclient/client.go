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
	"github.com/horizon/orion/services/orion-volume/internal/ports"
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

func (c *Client) GetHost(ctx context.Context, hostID string) (ports.HostRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/hosts/"+hostID, nil)
	if err != nil {
		return ports.HostRecord{}, err
	}
	httpx.SetRequestIDHeader(ctx, req)

	resp, err := retry.DoHTTP(ctx, c.client, req.Method, req.URL.String(), nil, req.Header)
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
		Host hostPayload `json:"host"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return ports.HostRecord{}, err
	}
	return response.Host.record(), nil
}

func (c *Client) ListHosts(ctx context.Context) ([]ports.HostRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/hosts", nil)
	if err != nil {
		return nil, err
	}
	httpx.SetRequestIDHeader(ctx, req)

	resp, err := retry.DoHTTP(ctx, c.client, req.Method, req.URL.String(), nil, req.Header)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("placement list hosts failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var response struct {
		Hosts []hostPayload `json:"hosts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	hosts := make([]ports.HostRecord, 0, len(response.Hosts))
	for _, host := range response.Hosts {
		hosts = append(hosts, host.record())
	}
	return hosts, nil
}

func (c *Client) SelectHost(ctx context.Context, req ports.SelectHostRequest) (ports.HostSelection, error) {
	payload := map[string]any{
		"server_id":                 req.ServerID,
		"cell_id":                   req.CellID,
		"vcpus":                     0,
		"memory_mb":                 0,
		"disk_gb":                   req.DiskGB,
		"traits_required":           []string{},
		"require_node_agent":        false,
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
		body, _ := io.ReadAll(resp.Body)
		return ports.HostSelection{}, fmt.Errorf("placement select host failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
		"vcpus":     0,
		"memory_mb": 0,
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
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("placement release host failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

type hostPayload struct {
	HostID             string `json:"host_id"`
	CellID             string `json:"cell_id"`
	Enabled            bool   `json:"enabled"`
	Drained            bool   `json:"drained"`
	VolumeHostAgentURL string `json:"volume_host_agent_url"`
}

func (h hostPayload) record() ports.HostRecord {
	return ports.HostRecord{
		HostID:             h.HostID,
		CellID:             h.CellID,
		Enabled:            h.Enabled,
		Drained:            h.Drained,
		VolumeHostAgentURL: h.VolumeHostAgentURL,
	}
}

var ErrHostNotFound = fmt.Errorf("host not found")
var ErrNoValidHost = fmt.Errorf("no valid host")
