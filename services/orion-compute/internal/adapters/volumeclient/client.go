package volumeclient

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
	"github.com/horizon/orion/libs/go/kit/retry"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
)

type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) GetVolume(ctx context.Context, volumeID string) (volumekit.Volume, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/volumes/"+volumeID, nil)
	if err != nil {
		return volumekit.Volume{}, err
	}
	httpx.SetRequestIDHeader(ctx, req)
	resp, err := retry.DoHTTP(ctx, c.client, req.Method, req.URL.String(), nil, req.Header)
	if err != nil {
		return volumekit.Volume{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return volumekit.Volume{}, ports.ErrVolumeNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return volumekit.Volume{}, fmt.Errorf("volume get failed with status %d", resp.StatusCode)
	}
	var response struct {
		Volume volumekit.Volume `json:"volume"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return volumekit.Volume{}, err
	}
	return response.Volume, nil
}

func (c *Client) CreateVolume(ctx context.Context, req volumekit.CreateVolumeRequest) (volumekit.Volume, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return volumekit.Volume{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/volumes", bytes.NewReader(body))
	if err != nil {
		return volumekit.Volume{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpx.SetRequestIDHeader(ctx, httpReq)
	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), body, httpReq.Header)
	if err != nil {
		return volumekit.Volume{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return volumekit.Volume{}, ports.ErrVolumeBackendUnavailable
	}
	if resp.StatusCode != http.StatusCreated {
		return volumekit.Volume{}, fmt.Errorf("volume create failed with status %d", resp.StatusCode)
	}
	var response struct {
		Volume volumekit.Volume `json:"volume"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return volumekit.Volume{}, err
	}
	return response.Volume, nil
}

func (c *Client) DeleteVolume(ctx context.Context, volumeID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/v1/volumes/"+volumeID, nil)
	if err != nil {
		return err
	}
	httpx.SetRequestIDHeader(ctx, req)
	resp, err := retry.DoHTTP(ctx, c.client, req.Method, req.URL.String(), nil, req.Header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ports.ErrVolumeNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		return ports.ErrVolumeInUse
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		return ports.ErrVolumeBackendUnavailable
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("volume delete failed with status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) AttachVolume(ctx context.Context, volumeID, serverID, hostID string) (volumekit.Volume, error) {
	body, err := json.Marshal(volumekit.AttachVolumeRequest{ServerID: serverID, HostID: hostID})
	if err != nil {
		return volumekit.Volume{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/volumes/"+volumeID+"/attach", bytes.NewReader(body))
	if err != nil {
		return volumekit.Volume{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	httpx.SetRequestIDHeader(ctx, req)
	resp, err := retry.DoHTTP(ctx, c.client, req.Method, req.URL.String(), body, req.Header)
	if err != nil {
		return volumekit.Volume{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return volumekit.Volume{}, ports.ErrVolumeNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		var envelope apierror.Envelope
		if json.NewDecoder(resp.Body).Decode(&envelope) == nil && envelope.Error.Code == "volume_in_use" {
			return volumekit.Volume{}, ports.ErrVolumeInUse
		}
		return volumekit.Volume{}, fmt.Errorf("volume attach failed with status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return volumekit.Volume{}, fmt.Errorf("volume attach failed with status %d", resp.StatusCode)
	}
	var response struct {
		Volume volumekit.Volume `json:"volume"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return volumekit.Volume{}, err
	}
	return response.Volume, nil
}

func (c *Client) DetachVolume(ctx context.Context, volumeID string) (volumekit.Volume, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/volumes/"+volumeID+"/detach", nil)
	if err != nil {
		return volumekit.Volume{}, err
	}
	httpx.SetRequestIDHeader(ctx, req)
	resp, err := retry.DoHTTP(ctx, c.client, req.Method, req.URL.String(), nil, req.Header)
	if err != nil {
		return volumekit.Volume{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return volumekit.Volume{}, ports.ErrVolumeNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		var envelope apierror.Envelope
		if json.NewDecoder(resp.Body).Decode(&envelope) == nil && envelope.Error.Code == "volume_not_attached" {
			return volumekit.Volume{}, ports.ErrVolumeNotAttached
		}
		return volumekit.Volume{}, fmt.Errorf("volume detach failed with status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return volumekit.Volume{}, fmt.Errorf("volume detach failed with status %d", resp.StatusCode)
	}
	var response struct {
		Volume volumekit.Volume `json:"volume"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return volumekit.Volume{}, err
	}
	return response.Volume, nil
}
