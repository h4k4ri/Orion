package plugin

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	v1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
	"github.com/horizon/orion/sdk/go/retry"
	"github.com/horizon/orion/sdk/go/tls"
)

// Client is the client-side adapter for the Orion PluginExecutor protocol.
// The ResourceService client is retained for backwards compatibility.
type Client struct {
	conn      *grpc.ClientConn
	executor  v1.PluginExecutorClient
	resources v1.ResourceServiceClient
	config    ClientConfig
}

type ClientConfig struct {
	Endpoint  string
	TLSConfig *tls.TLSConfig
	Timeout   time.Duration
	Retry     *RetryConfig
}

type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func NewClient(ctx context.Context, cfg ClientConfig) (*Client, error) {
	var opts []grpc.DialOption
	if cfg.TLSConfig != nil {
		creds, err := tls.LoadTLSConfig(cfg.TLSConfig)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.DialContext(ctx, cfg.Endpoint, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:      conn,
		executor:  v1.NewPluginExecutorClient(conn),
		resources: v1.NewResourceServiceClient(conn),
		config:    cfg,
	}, nil
}

func (c *Client) Invoke(ctx context.Context, req *v1.InvokeRequest) (*v1.InvokeResponse, error) {
	var response *v1.InvokeResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.executor.Invoke(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) InvokeAsync(ctx context.Context, req *v1.InvokeRequest) (*v1.OperationStatus, error) {
	var response *v1.OperationStatus
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.executor.InvokeAsync(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) GetOperationStatus(ctx context.Context, req *v1.GetOperationStatusRequest) (*v1.OperationStatus, error) {
	var response *v1.OperationStatus
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.executor.GetOperationStatus(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) CancelOperation(ctx context.Context, req *v1.CancelOperationRequest) (*v1.OperationStatus, error) {
	var response *v1.OperationStatus
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.executor.CancelOperation(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) Reconcile(ctx context.Context, req *v1.ReconcileRequest) (*v1.ReconcileResponse, error) {
	var response *v1.ReconcileResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.executor.Reconcile(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) HealthCheck(ctx context.Context, req *v1.HealthCheckRequest) (*v1.HealthCheckResponse, error) {
	var response *v1.HealthCheckResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.executor.HealthCheck(callCtx, req)
		return err
	})
	return response, err
}

// These methods preserve the original core ResourceService client surface.
func (c *Client) CreateResource(ctx context.Context, req *v1.CreateResourceRequest) (*v1.CreateResourceResponse, error) {
	var response *v1.CreateResourceResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.CreateResource(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) GetResource(ctx context.Context, req *v1.GetResourceRequest) (*v1.Resource, error) {
	var response *v1.Resource
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.GetResource(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) UpdateResource(ctx context.Context, req *v1.UpdateResourceRequest) (*v1.Resource, error) {
	var response *v1.Resource
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.UpdateResource(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) DeleteResource(ctx context.Context, req *v1.DeleteResourceRequest) (*v1.Resource, error) {
	var response *v1.Resource
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.DeleteResource(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) ListResources(ctx context.Context, req *v1.ListResourcesRequest) (*v1.ListResourcesResponse, error) {
	var response *v1.ListResourcesResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.ListResources(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) ListResourcesByKind(ctx context.Context, req *v1.ListResourcesByKindRequest) (*v1.ListResourcesResponse, error) {
	var response *v1.ListResourcesResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.ListResourcesByKind(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) ImportResource(ctx context.Context, req *v1.ImportResourceRequest) (*v1.ImportResourceResponse, error) {
	var response *v1.ImportResourceResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.ImportResource(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) CreateRelationship(ctx context.Context, req *v1.CreateRelationshipRequest) (*v1.ResourceRelationship, error) {
	var response *v1.ResourceRelationship
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.CreateRelationship(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) GetRelationship(ctx context.Context, req *v1.GetRelationshipRequest) (*v1.ResourceRelationship, error) {
	var response *v1.ResourceRelationship
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.GetRelationship(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) DeleteRelationship(ctx context.Context, req *v1.DeleteRelationshipRequest) (*v1.ResourceRelationship, error) {
	var response *v1.ResourceRelationship
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.DeleteRelationship(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) ListRelationships(ctx context.Context, req *v1.ListRelationshipsRequest) (*v1.ListRelationshipsResponse, error) {
	var response *v1.ListRelationshipsResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.ListRelationships(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) ObserveResource(ctx context.Context, req *v1.ObserveResourceRequest) (*v1.Resource, error) {
	var response *v1.Resource
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.ObserveResource(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) DiscoverUnmanaged(ctx context.Context, req *v1.DiscoverUnmanagedRequest) (*v1.DiscoverResponse, error) {
	var response *v1.DiscoverResponse
	err := c.call(ctx, func(callCtx context.Context) error {
		var err error
		response, err = c.resources.DiscoverUnmanaged(callCtx, req)
		return err
	})
	return response, err
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) call(ctx context.Context, fn func(context.Context) error) error {
	callCtx := ctx
	var cancel context.CancelFunc
	if c.config.Timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, c.config.Timeout)
		defer cancel()
	}
	if c.config.Retry == nil {
		return fn(callCtx)
	}
	cfg := retry.Config{
		MaxAttempts:  c.config.Retry.MaxAttempts,
		InitialDelay: c.config.Retry.InitialDelay,
		MaxDelay:     c.config.Retry.MaxDelay,
		Multiplier:   2,
		Jitter:       true,
	}
	if cfg.MaxAttempts <= 0 {
		cfg = retry.DefaultConfig
	}
	return retry.Do(callCtx, cfg, func() error { return fn(callCtx) })
}
