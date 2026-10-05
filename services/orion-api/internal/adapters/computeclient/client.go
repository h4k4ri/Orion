package computeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/retry"
	"github.com/horizon/orion/libs/go/kit/task"
	"github.com/horizon/orion/services/orion-api/internal/ports"
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

func (c *Client) CreateServer(ctx context.Context, actor authn.Actor, req compute.CreateServerRequest) (compute.Server, task.Task, error) {
	response, err := c.CreateServerAsync(ctx, actor, req)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	return compute.Server{
		ID:        response.ServerID,
		ProjectID: actor.Scope.ProjectID,
		Status:    response.Status,
	}, task.Task{ID: response.OperationID, Kind: "server.create", Status: task.StatusRunning}, nil
}

func (c *Client) CreateServerAsync(ctx context.Context, actor authn.Actor, req compute.CreateServerRequest) (compute.CreateServerResponseAsync, error) {
	payload := map[string]any{
		"actor":   actor,
		"request": req,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return compute.CreateServerResponseAsync{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/servers", bytes.NewReader(body))
	if err != nil {
		return compute.CreateServerResponseAsync{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), body, httpReq.Header)
	if err != nil {
		return compute.CreateServerResponseAsync{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted:
	case http.StatusBadRequest:
		return compute.CreateServerResponseAsync{}, ports.ErrUnknownFlavor
	case http.StatusNotFound:
		var envelope apierror.Envelope
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err == nil && envelope.Error.Code == "network_not_found" {
			return compute.CreateServerResponseAsync{}, ports.ErrNetworkNotFound
		}
		return compute.CreateServerResponseAsync{}, ports.ErrImageNotFound
	case http.StatusServiceUnavailable:
		var envelope apierror.Envelope
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err == nil && envelope.Error.Code == "backend_unavailable" {
			return compute.CreateServerResponseAsync{}, ports.ErrNetworkBackendUnavailable
		}
		return compute.CreateServerResponseAsync{}, fmt.Errorf("compute create failed with status %d", resp.StatusCode)
	case http.StatusConflict:
		return compute.CreateServerResponseAsync{}, ports.ErrNoValidHost
	case http.StatusTooManyRequests:
		return compute.CreateServerResponseAsync{}, ports.ErrQuotaExceeded
	default:
		return compute.CreateServerResponseAsync{}, fmt.Errorf("compute create failed with status %d", resp.StatusCode)
	}

	var response compute.CreateServerResponseAsync
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return compute.CreateServerResponseAsync{}, err
	}

	return response, nil
}

func (c *Client) ListServers(ctx context.Context, actor authn.Actor) ([]compute.Server, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/servers", nil)
	if err != nil {
		return nil, err
	}
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), nil, httpReq.Header)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("compute list servers failed with status %d", resp.StatusCode)
	}

	var response struct {
		Servers []compute.Server `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}
	return response.Servers, nil
}

func (c *Client) GetServer(ctx context.Context, actor authn.Actor, serverID string) (compute.Server, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/servers/"+serverID, nil)
	if err != nil {
		return compute.Server{}, err
	}
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), nil, httpReq.Header)
	if err != nil {
		return compute.Server{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return compute.Server{}, ports.ErrServerNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return compute.Server{}, fmt.Errorf("compute get server failed with status %d", resp.StatusCode)
	}

	var response struct {
		Server compute.Server `json:"server"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return compute.Server{}, err
	}

	return response.Server, nil
}

func (c *Client) DeleteServer(ctx context.Context, actor authn.Actor, serverID string) (task.Task, error) {
	payload := map[string]any{"actor": actor}
	body, err := json.Marshal(payload)
	if err != nil {
		return task.Task{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/v1/servers/"+serverID, bytes.NewReader(body))
	if err != nil {
		return task.Task{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), body, httpReq.Header)
	if err != nil {
		return task.Task{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return task.Task{}, ports.ErrServerNotFound
	}
	if resp.StatusCode != http.StatusAccepted {
		return task.Task{}, fmt.Errorf("compute delete server failed with status %d", resp.StatusCode)
	}
	var response struct {
		Task task.Task `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return task.Task{}, err
	}
	return response.Task, nil
}

func (c *Client) AttachVolume(ctx context.Context, actor authn.Actor, serverID, volumeID string) (compute.Server, task.Task, error) {
	return c.doServerVolumeAction(ctx, actor, serverID, volumeID, "attach-volume")
}

func (c *Client) DetachVolume(ctx context.Context, actor authn.Actor, serverID, volumeID string) (compute.Server, task.Task, error) {
	return c.doServerVolumeAction(ctx, actor, serverID, volumeID, "detach-volume")
}

func (c *Client) GetTask(ctx context.Context, actor authn.Actor, taskID string) (task.Task, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/tasks/"+taskID, nil)
	if err != nil {
		return task.Task{}, err
	}
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), nil, httpReq.Header)
	if err != nil {
		return task.Task{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return task.Task{}, ports.ErrTaskNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return task.Task{}, fmt.Errorf("compute get task failed with status %d", resp.StatusCode)
	}

	var response struct {
		Task task.Task `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return task.Task{}, err
	}

	return response.Task, nil
}

func (c *Client) doServerVolumeAction(ctx context.Context, actor authn.Actor, serverID, volumeID, action string) (compute.Server, task.Task, error) {
	payload := map[string]any{
		"actor": actor,
		"request": compute.AttachVolumeRequest{
			VolumeID: volumeID,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/servers/"+serverID+"/"+action, bytes.NewReader(body))
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpx.SetRequestIDHeader(ctx, httpReq)

	resp, err := retry.DoHTTP(ctx, c.client, httpReq.Method, httpReq.URL.String(), body, httpReq.Header)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted:
	case http.StatusNotFound:
		var envelope apierror.Envelope
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err == nil && envelope.Error.Code == "volume_not_found" {
			return compute.Server{}, task.Task{}, ports.ErrVolumeNotFound
		}
		return compute.Server{}, task.Task{}, ports.ErrServerNotFound
	case http.StatusConflict:
		var envelope apierror.Envelope
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err == nil {
			switch envelope.Error.Code {
			case "volume_in_use":
				return compute.Server{}, task.Task{}, ports.ErrVolumeInUse
			case "volume_not_attached":
				return compute.Server{}, task.Task{}, ports.ErrVolumeNotAttached
			}
		}
		return compute.Server{}, task.Task{}, fmt.Errorf("compute server volume action failed with status %d", resp.StatusCode)
	case http.StatusServiceUnavailable:
		return compute.Server{}, task.Task{}, ports.ErrVolumeBackendUnavailable
	default:
		return compute.Server{}, task.Task{}, fmt.Errorf("compute server volume action failed with status %d", resp.StatusCode)
	}

	var response compute.CreateServerResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return compute.Server{}, task.Task{}, err
	}
	return response.Server, response.Task, nil
}
