package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	v1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
	oriontls "github.com/horizon/orion/sdk/go/tls"
)

type Config struct {
	Endpoint  string
	TLSConfig *oriontls.TLSConfig
	Timeout   time.Duration
	Retry     *RetryConfig
}

type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

type Client struct {
	conn   *grpc.ClientConn
	client v1.ResourceServiceClient
	config Config
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	var opts []grpc.DialOption

	if cfg.TLSConfig != nil {
		creds, err := oriontls.LoadTLSConfig(cfg.TLSConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS config: %w", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.DialContext(ctx, cfg.Endpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %w", err)
	}

	return &Client{
		conn:   conn,
		client: v1.NewResourceServiceClient(conn),
		config: cfg,
	}, nil
}

func (c *Client) CreateResource(ctx context.Context, req *v1.CreateResourceRequest, opts ...grpc.CallOption) (*v1.CreateResourceResponse, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.CreateResource(ctx, req, opts...)
}

func (c *Client) GetResource(ctx context.Context, req *v1.GetResourceRequest, opts ...grpc.CallOption) (*v1.Resource, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.GetResource(ctx, req, opts...)
}

func (c *Client) UpdateResource(ctx context.Context, req *v1.UpdateResourceRequest, opts ...grpc.CallOption) (*v1.Resource, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.UpdateResource(ctx, req, opts...)
}

func (c *Client) DeleteResource(ctx context.Context, req *v1.DeleteResourceRequest, opts ...grpc.CallOption) (*v1.Resource, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.DeleteResource(ctx, req, opts...)
}

func (c *Client) ListResources(ctx context.Context, req *v1.ListResourcesRequest, opts ...grpc.CallOption) (*v1.ListResourcesResponse, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.ListResources(ctx, req, opts...)
}

func (c *Client) ListResourcesByKind(ctx context.Context, req *v1.ListResourcesByKindRequest, opts ...grpc.CallOption) (*v1.ListResourcesResponse, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.ListResourcesByKind(ctx, req, opts...)
}

func (c *Client) ImportResource(ctx context.Context, req *v1.ImportResourceRequest, opts ...grpc.CallOption) (*v1.ImportResourceResponse, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.ImportResource(ctx, req, opts...)
}

func (c *Client) CreateRelationship(ctx context.Context, req *v1.CreateRelationshipRequest, opts ...grpc.CallOption) (*v1.ResourceRelationship, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.CreateRelationship(ctx, req, opts...)
}

func (c *Client) GetRelationship(ctx context.Context, req *v1.GetRelationshipRequest, opts ...grpc.CallOption) (*v1.ResourceRelationship, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.GetRelationship(ctx, req, opts...)
}

func (c *Client) DeleteRelationship(ctx context.Context, req *v1.DeleteRelationshipRequest, opts ...grpc.CallOption) (*v1.ResourceRelationship, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.DeleteRelationship(ctx, req, opts...)
}

func (c *Client) ListRelationships(ctx context.Context, req *v1.ListRelationshipsRequest, opts ...grpc.CallOption) (*v1.ListRelationshipsResponse, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.ListRelationships(ctx, req, opts...)
}

func (c *Client) ObserveResource(ctx context.Context, req *v1.ObserveResourceRequest, opts ...grpc.CallOption) (*v1.Resource, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.ObserveResource(ctx, req, opts...)
}

func (c *Client) DiscoverUnmanaged(ctx context.Context, req *v1.DiscoverUnmanagedRequest, opts ...grpc.CallOption) (*v1.DiscoverResponse, error) {
	var cancel context.CancelFunc
	ctx, cancel = c.withTimeout(ctx)
	defer cancel()
	return c.client.DiscoverUnmanaged(ctx, req, opts...)
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.config.Timeout > 0 {
		return context.WithTimeout(ctx, c.config.Timeout)
	}
	return ctx, func() {}
}
