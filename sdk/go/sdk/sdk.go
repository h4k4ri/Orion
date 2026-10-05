package sdk

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/horizon/orion/sdk/go/health"
	"github.com/horizon/orion/sdk/go/metrics"
	"github.com/horizon/orion/sdk/go/middleware"
	"github.com/horizon/orion/sdk/go/plugin"
	v1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
	"github.com/horizon/orion/sdk/go/retry"
	"github.com/horizon/orion/sdk/go/tls"
)

type PluginSDK struct {
	plugin  *plugin.Plugin
	server  *plugin.Server
	config  Config
	health  *health.GRPCServer
	logger  *middleware.Logging
	metrics *metrics.Recorder
}

type Config struct {
	Endpoint string
	TLS      *tls.Config
	Timeout  time.Duration
	Retry    retry.Config
	Health   bool
	Metrics  bool
}

func New(p *plugin.Plugin, cfg Config) (*PluginSDK, error) {
	sdk := &PluginSDK{
		plugin:  p,
		config:  cfg,
		health:  health.NewGRPCServer(),
		metrics: metrics.NewRecorder(),
		logger:  middleware.NewLogging(slog.Default()),
	}

	var serverOpts []plugin.ServerOption

	if cfg.Metrics {
		serverOpts = append(serverOpts, plugin.WithUnaryInterceptor(middleware.NewMetrics(sdk.metrics).UnaryServerInterceptor()))
	}

	if cfg.Health {
		serverOpts = append(serverOpts, plugin.WithHealthServer(sdk.health))
	}

	serverOpts = append(serverOpts, plugin.WithLogger(sdk.logger), plugin.WithTimeout(cfg.Timeout), plugin.WithRetry(cfg.Retry))

	sdk.server = plugin.NewServer(p, cfg.Endpoint, serverOpts...)

	return sdk, nil
}

func (s *PluginSDK) RegisterHealthCheck(name string, check health.Checker) {
	s.health.Register(name, check)
}

func (s *PluginSDK) RecordMetric(method string, duration time.Duration, success bool) {
	s.metrics.Record(method, duration, success)
}

func (s *PluginSDK) Serve(ctx context.Context) error {
	if s.server == nil {
		return fmt.Errorf("server not initialized")
	}
	return s.server.Serve(ctx)
}

func (s *PluginSDK) ServeAndWait() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Serve(ctx)
	}()

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
		return nil
	}
}

func (s *PluginSDK) GetMetrics() map[string]metrics.MethodSnapshot {
	return s.metrics.Snapshot()
}

func (s *PluginSDK) GetHealth() (health.Status, map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, details, _ := s.health.Check(ctx)
	return status, details
}

func (s *PluginSDK) Stop() {
	if s.server != nil {
		s.server.Stop()
	}
}

type ClientConfig struct {
	Endpoint string
	TLS      *tls.Config
	Timeout  time.Duration
	Retry    retry.Config
}

func NewClient(cfg ClientConfig) (*Client, error) {
	var clientTLS *tls.TLSConfig
	if cfg.TLS != nil {
		clientTLS = &tls.TLSConfig{
			CaCert:     cfg.TLS.CaCertFile,
			ClientCert: cfg.TLS.ClientCertFile,
			ClientKey:  cfg.TLS.ClientKeyFile,
		}
	}
	c, err := plugin.NewClient(context.Background(), plugin.ClientConfig{
		Endpoint:  cfg.Endpoint,
		TLSConfig: clientTLS,
		Timeout:   cfg.Timeout,
		Retry:     &plugin.RetryConfig{MaxAttempts: cfg.Retry.MaxAttempts, InitialDelay: cfg.Retry.InitialDelay, MaxDelay: cfg.Retry.MaxDelay},
	})
	if err != nil {
		return nil, err
	}
	return &Client{client: c}, nil
}

type Client struct {
	client *plugin.Client
}

func (c *Client) Invoke(ctx context.Context, req *v1.InvokeRequest) (*v1.InvokeResponse, error) {
	return c.client.Invoke(ctx, req)
}

func (c *Client) InvokeAsync(ctx context.Context, req *v1.InvokeRequest) (*v1.OperationStatus, error) {
	return c.client.InvokeAsync(ctx, req)
}

func (c *Client) GetOperationStatus(ctx context.Context, req *v1.GetOperationStatusRequest) (*v1.OperationStatus, error) {
	return c.client.GetOperationStatus(ctx, req)
}

func (c *Client) CancelOperation(ctx context.Context, req *v1.CancelOperationRequest) (*v1.OperationStatus, error) {
	return c.client.CancelOperation(ctx, req)
}

func (c *Client) Reconcile(ctx context.Context, req *v1.ReconcileRequest) (*v1.ReconcileResponse, error) {
	return c.client.Reconcile(ctx, req)
}

func (c *Client) HealthCheck(ctx context.Context, req *v1.HealthCheckRequest) (*v1.HealthCheckResponse, error) {
	return c.client.HealthCheck(ctx, req)
}

func (c *Client) Close() error {
	return c.client.Close()
}

type RetryConfig = retry.Config
type TLSConfig = tls.Config
