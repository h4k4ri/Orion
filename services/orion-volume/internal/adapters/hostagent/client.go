package hostagent

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	volumev1 "github.com/horizon/orion/gen/go/volume/v1"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/retry"
	"github.com/horizon/orion/libs/go/kit/tlsconfig"
	"github.com/horizon/orion/services/orion-volume/internal/ports"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Client struct {
	fallbackAddr string
	hosts        ports.HostDirectory
	mu           sync.Mutex
	connections  map[string]*grpc.ClientConn
}

func New(fallbackEndpoint string, hosts ports.HostDirectory) *Client {
	return &Client{fallbackAddr: normalizeAddr(fallbackEndpoint), hosts: hosts, connections: make(map[string]*grpc.ClientConn)}
}

func (c *Client) CreateVolume(ctx context.Context, hostID string, req ports.CreateVolumeRequest) (ports.CreateVolumeResult, error) {
	client, err := c.client(ctx, hostID)
	if err != nil {
		return ports.CreateVolumeResult{}, err
	}
	var response *volumev1.VolumeResponse
	err = retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.CreateVolume(withRequestID(callCtx), &volumev1.CreateVolumeRequest{VolumeId: req.VolumeID, SizeGb: int32(req.SizeGB)})
		return callErr
	})
	if err != nil {
		return ports.CreateVolumeResult{}, mapError(err)
	}
	return ports.CreateVolumeResult{DevicePath: response.GetDevicePath()}, nil
}

func (c *Client) DeleteVolume(ctx context.Context, hostID, volumeID string) error {
	client, err := c.client(ctx, hostID)
	if err != nil {
		return err
	}
	err = retryRPC(ctx, func(callCtx context.Context) error {
		_, callErr := client.DeleteVolume(withRequestID(callCtx), &volumev1.DeleteVolumeRequest{VolumeId: volumeID})
		return callErr
	})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (c *Client) ObserveVolume(ctx context.Context, hostID, volumeID string) (ports.VolumeObservation, error) {
	client, err := c.client(ctx, hostID)
	if err != nil {
		return ports.VolumeObservation{}, err
	}
	var response *volumev1.VolumeResponse
	err = retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = client.ObserveVolume(withRequestID(callCtx), &volumev1.ObserveVolumeRequest{VolumeId: volumeID})
		return callErr
	})
	if status.Code(err) == codes.NotFound {
		return ports.VolumeObservation{Exists: false}, nil
	}
	if err != nil {
		return ports.VolumeObservation{}, mapError(err)
	}
	return ports.VolumeObservation{Exists: response.GetExists(), DevicePath: response.GetDevicePath()}, nil
}

func (c *Client) CreateSnapshot(ctx context.Context, hostID, volumeID, snapshotID string) error {
	client, err := c.client(ctx, hostID)
	if err != nil {
		return err
	}
	err = retryRPC(ctx, func(callCtx context.Context) error {
		_, callErr := client.CreateSnapshot(withRequestID(callCtx), &volumev1.CreateSnapshotRequest{VolumeId: volumeID, SnapshotId: snapshotID})
		return callErr
	})
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (c *Client) DeleteSnapshot(ctx context.Context, hostID, volumeID, snapshotID string) error {
	client, err := c.client(ctx, hostID)
	if err != nil {
		return err
	}
	err = retryRPC(ctx, func(callCtx context.Context) error {
		_, callErr := client.DeleteSnapshot(withRequestID(callCtx), &volumev1.DeleteSnapshotRequest{VolumeId: volumeID, SnapshotId: snapshotID})
		return callErr
	})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (c *Client) RestoreSnapshot(ctx context.Context, hostID, volumeID, snapshotID string) error {
	client, err := c.client(ctx, hostID)
	if err != nil {
		return err
	}
	err = retryRPC(ctx, func(callCtx context.Context) error {
		_, callErr := client.RestoreSnapshot(withRequestID(callCtx), &volumev1.RestoreSnapshotRequest{VolumeId: volumeID, SnapshotId: snapshotID})
		return callErr
	})
	if err != nil {
		return mapError(err)
	}
	return nil
}

func (c *Client) client(ctx context.Context, hostID string) (volumev1.VolumeHostAgentClient, error) {
	addr, err := c.resolveAddr(ctx, hostID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	conn := c.connections[addr]
	if conn == nil {
		transport := insecure.NewCredentials()
		if configured, tlsErr := tlsconfig.ClientCredentialsFromEnv(); tlsErr != nil {
			return nil, tlsErr
		} else if configured != nil {
			transport = configured
		}
		conn, err = grpc.NewClient(addr, grpc.WithTransportCredentials(transport), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
		if err != nil {
			return nil, fmt.Errorf("connect volume host agent: %w", err)
		}
		c.connections[addr] = conn
	}
	return volumev1.NewVolumeHostAgentClient(conn), nil
}

func (c *Client) resolveAddr(ctx context.Context, hostID string) (string, error) {
	if hostID != "" && c.hosts != nil {
		host, err := c.hosts.GetHost(ctx, hostID)
		if err == nil && strings.TrimSpace(host.VolumeHostAgentURL) != "" {
			return normalizeAddr(host.VolumeHostAgentURL), nil
		}
		if err != nil && c.fallbackAddr == "" {
			return "", err
		}
	}
	if c.fallbackAddr != "" {
		return c.fallbackAddr, nil
	}
	return "", fmt.Errorf("volume host agent endpoint not found for host %s", hostID)
}

func normalizeAddr(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		endpoint = parsed.Host
	}
	endpoint = strings.TrimPrefix(endpoint, "grpc://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	if strings.HasSuffix(endpoint, ":8088") {
		endpoint = strings.TrimSuffix(endpoint, ":8088") + ":50055"
	}
	return strings.TrimRight(endpoint, "/")
}

func withRequestID(ctx context.Context) context.Context {
	if requestID := httpx.RequestIDFromContext(ctx); requestID != "" {
		return metadata.AppendToOutgoingContext(ctx, "x-request-id", requestID)
	}
	return ctx
}

func mapError(err error) error {
	if status.Code(err) == codes.Unavailable {
		return ports.ErrBackendUnavailable
	}
	return err
}

func retryRPC(ctx context.Context, operation func(context.Context) error) error {
	policy := retry.Policy{MaxAttempts: 3, Initial: 100 * time.Millisecond, MaxDelay: time.Second}
	return retry.Do(ctx, policy, operation, func(err error) bool {
		code := status.Code(err)
		return code == codes.Unavailable || code == codes.ResourceExhausted || code == codes.DeadlineExceeded
	})
}
