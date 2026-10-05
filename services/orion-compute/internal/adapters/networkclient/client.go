package networkclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/httpx"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
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
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) CreatePort(ctx context.Context, req networkkit.CreatePortRequest) (networkkit.Port, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return networkkit.Port{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/ports", bytes.NewReader(body))
	if err != nil {
		return networkkit.Port{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), body, httpReq.Header)
	if err != nil {
		return networkkit.Port{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		var envelope apierror.Envelope
		if json.NewDecoder(resp.Body).Decode(&envelope) == nil && envelope.Error.Code == "network_not_found" {
			return networkkit.Port{}, ports.ErrNetworkNotFound
		}
		return networkkit.Port{}, fmt.Errorf("network create port failed with status %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		var envelope apierror.Envelope
		if json.NewDecoder(resp.Body).Decode(&envelope) == nil && envelope.Error.Code == "backend_unavailable" {
			return networkkit.Port{}, ports.ErrNetworkBackendUnavailable
		}
		return networkkit.Port{}, fmt.Errorf("network create port failed with status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusCreated {
		return networkkit.Port{}, fmt.Errorf("network create port failed with status %d", resp.StatusCode)
	}

	var response struct {
		Port networkkit.Port `json:"port"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return networkkit.Port{}, err
	}
	return response.Port, nil
}

func (c *Client) DeletePort(ctx context.Context, portID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/v1/ports/"+portID, nil)
	if err != nil {
		return err
	}
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), nil, httpReq.Header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ports.ErrNetworkNotFound
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		var envelope apierror.Envelope
		if json.NewDecoder(resp.Body).Decode(&envelope) == nil && envelope.Error.Code == "backend_unavailable" {
			return ports.ErrNetworkBackendUnavailable
		}
		return fmt.Errorf("network delete port failed with status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("network delete port failed with status %d", resp.StatusCode)
	}
	return nil
}
