package nodeagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "github.com/horizon/orion/gen/go/common/v1"
	nodev1 "github.com/horizon/orion/gen/go/node/v1"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/image"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/libs/go/kit/retry"
	"github.com/horizon/orion/libs/go/kit/tlsconfig"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Client struct {
	fallbackURL  string
	imageBaseURL string
	hosts        ports.HostDirectory
	conns        map[string]*grpc.ClientConn
}

func New(fallbackURL, imageBaseURL string, hosts ports.HostDirectory) *Client {
	return &Client{
		fallbackURL:  strings.TrimRight(fallbackURL, "/"),
		imageBaseURL: strings.TrimRight(imageBaseURL, "/"),
		hosts:        hosts,
		conns:        make(map[string]*grpc.ClientConn),
	}
}

func (c *Client) getClient(ctx context.Context, hostID string) (nodev1.NodeAgentServiceClient, error) {
	addr, err := c.resolveAddr(ctx, hostID)
	if err != nil {
		return nil, err
	}

	if conn, ok := c.conns[addr]; ok {
		return nodev1.NewNodeAgentServiceClient(conn), nil
	}

	transport := insecure.NewCredentials()
	if configured, tlsErr := tlsconfig.ClientCredentialsFromEnv(); tlsErr != nil {
		return nil, tlsErr
	} else if configured != nil {
		transport = configured
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(transport), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to node agent at %s: %w", addr, err)
	}
	c.conns[addr] = conn
	return nodev1.NewNodeAgentServiceClient(conn), nil
}

func (c *Client) BuildServer(ctx context.Context, server compute.Server, img image.Image, ports []networkkit.Port, vcpus, memoryMB, diskGB int) (compute.Server, error) {
	ctx = outgoingContext(ctx)
	client, err := c.getClient(ctx, server.HostID)
	if err != nil {
		return compute.Server{}, err
	}

	var networkPorts []*nodev1.PortSpec
	for _, p := range ports {
		networkPorts = append(networkPorts, &nodev1.PortSpec{
			Id:         &v1.PortID{Value: p.ID},
			NetworkId:  p.NetworkID,
			MacAddress: p.MACAddress,
		})
	}

	req := &nodev1.BuildInstanceRequest{
		InstanceId: &v1.InstanceID{Value: server.ID},
		HostId:     &v1.NodeID{Value: server.HostID},
		Name:       server.Name,
		Spec: &nodev1.InstanceSpec{
			Vcpus:    int32(vcpus),
			MemoryMb: int64(memoryMB),
			DiskGb:   int64(diskGB),
			Image: &nodev1.ImageSpec{
				Id:             &v1.ResourceID{Value: img.ID},
				SourcePath:     img.Path,
				ChecksumSha256: img.ChecksumSHA256,
				SizeBytes:      img.SizeBytes,
				SourceUrl:      c.imageBaseURL + "/v1/images/" + img.ID + "/file",
			},
			Ports: networkPorts,
		},
	}

	var resp *nodev1.BuildInstanceResponse
	err = retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		resp, callErr = client.BuildInstance(callCtx, req)
		return callErr
	})
	if err != nil {
		return compute.Server{}, fmt.Errorf("node agent build failed: %w", err)
	}

	server.Status = resp.Status
	server.UpdatedAt = time.Now().UTC()
	return server, nil
}

func (c *Client) DeleteServer(ctx context.Context, server compute.Server) error {
	ctx = outgoingContext(ctx)
	client, err := c.getClient(ctx, server.HostID)
	if err != nil {
		return err
	}

	req := &nodev1.DestroyInstanceRequest{
		InstanceId: &v1.InstanceID{Value: server.ID},
		HostId:     &v1.NodeID{Value: server.HostID},
	}

	var resp *nodev1.DestroyInstanceResponse
	err = retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		resp, callErr = client.DestroyInstance(callCtx, req)
		return callErr
	})
	if err != nil {
		return fmt.Errorf("node agent delete failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("node agent delete failed: %s", resp.Error)
	}
	return nil
}

func (c *Client) AttachVolume(ctx context.Context, server compute.Server, volume volumekit.Volume) error {
	ctx = outgoingContext(ctx)
	client, err := c.getClient(ctx, server.HostID)
	if err != nil {
		return err
	}

	req := &nodev1.AttachVolumeRequest{
		InstanceId: &v1.InstanceID{Value: server.ID},
		HostId:     &v1.NodeID{Value: server.HostID},
		VolumeId:   &v1.VolumeID{Value: volume.ID},
		DevicePath: volume.DevicePath,
	}

	var resp *nodev1.AttachVolumeResponse
	err = retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		resp, callErr = client.AttachVolume(callCtx, req)
		return callErr
	})
	if err != nil {
		return fmt.Errorf("node agent attach volume failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("node agent attach volume failed: %s", resp.Error)
	}
	return nil
}

func (c *Client) DetachVolume(ctx context.Context, server compute.Server, volume volumekit.Volume) error {
	ctx = outgoingContext(ctx)
	client, err := c.getClient(ctx, server.HostID)
	if err != nil {
		return err
	}

	req := &nodev1.DetachVolumeRequest{
		InstanceId: &v1.InstanceID{Value: server.ID},
		HostId:     &v1.NodeID{Value: server.HostID},
		VolumeId:   &v1.VolumeID{Value: volume.ID},
	}

	var resp *nodev1.DetachVolumeResponse
	err = retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		resp, callErr = client.DetachVolume(callCtx, req)
		return callErr
	})
	if err != nil {
		return fmt.Errorf("node agent detach volume failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("node agent detach volume failed: %s", resp.Error)
	}
	return nil
}

func (c *Client) ObserveServer(ctx context.Context, server compute.Server) (ports.ServerObservation, error) {
	ctx = outgoingContext(ctx)
	client, err := c.getClient(ctx, server.HostID)
	if err != nil {
		return ports.ServerObservation{}, err
	}

	req := &nodev1.ObserveInstanceRequest{
		InstanceId: &v1.InstanceID{Value: server.ID},
		HostId:     &v1.NodeID{Value: server.HostID},
	}

	var resp *nodev1.ObserveInstanceResponse
	err = retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		resp, callErr = client.ObserveInstance(callCtx, req)
		return callErr
	})
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NOT_FOUND") {
			return ports.ServerObservation{}, ports.ErrServerInstanceNotFound
		}
		return ports.ServerObservation{}, fmt.Errorf("node agent observe failed: %w", err)
	}

	return ports.ServerObservation{Status: resp.Status}, nil
}

func outgoingContext(ctx context.Context) context.Context {
	md := metadata.New(map[string]string{})
	if requestID := httpx.RequestIDFromContext(ctx); requestID != "" {
		md.Set("x-request-id", requestID)
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	for key, value := range carrier {
		md.Set(key, value)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

func retryRPC(ctx context.Context, operation func(context.Context) error) error {
	policy := retry.Policy{MaxAttempts: 3, Initial: 100 * time.Millisecond, MaxDelay: time.Second}
	return retry.Do(ctx, policy, operation, func(err error) bool {
		switch status.Code(err) {
		case codes.Unavailable, codes.ResourceExhausted, codes.DeadlineExceeded:
			return true
		default:
			return false
		}
	})
}

func (c *Client) resolveAddr(ctx context.Context, hostID string) (string, error) {
	hostID = strings.TrimSpace(hostID)
	if hostID != "" && c.hosts != nil {
		host, err := c.hosts.GetHost(ctx, hostID)
		if err == nil && strings.TrimSpace(host.NodeAgentGRPCURL) != "" {
			return strings.TrimRight(host.NodeAgentGRPCURL, ":"), nil
		}
		if err != nil && c.fallbackURL == "" {
			return "", err
		}
	}

	if c.fallbackURL != "" {
		return c.fallbackURL, nil
	}
	if hostID == "" {
		return "", fmt.Errorf("node agent host is required")
	}
	return "", fmt.Errorf("node agent endpoint not found for host %s", hostID)
}
