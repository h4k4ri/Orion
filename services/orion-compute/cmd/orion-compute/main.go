package main

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/horizon/orion/libs/go/kit/config"
	"github.com/horizon/orion/libs/go/kit/events"
	"github.com/horizon/orion/libs/go/kit/logging"
	"github.com/horizon/orion/libs/go/kit/natsx"
	"github.com/horizon/orion/libs/go/kit/observability"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	httpadapter "github.com/horizon/orion/services/orion-compute/internal/adapters/http"
	imageclient "github.com/horizon/orion/services/orion-compute/internal/adapters/imageclient"
	natsadapter "github.com/horizon/orion/services/orion-compute/internal/adapters/nats"
	networkclient "github.com/horizon/orion/services/orion-compute/internal/adapters/networkclient"
	nodeagent "github.com/horizon/orion/services/orion-compute/internal/adapters/nodeagent"
	operationclient "github.com/horizon/orion/services/orion-compute/internal/adapters/operationclient"
	placementclient "github.com/horizon/orion/services/orion-compute/internal/adapters/placementclient"
	pgstore "github.com/horizon/orion/services/orion-compute/internal/adapters/postgres"
	volumeclient "github.com/horizon/orion/services/orion-compute/internal/adapters/volumeclient"
	"github.com/horizon/orion/services/orion-compute/internal/application"
)

func main() {
	logger := logging.New("orion-compute")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	obs, err := observability.New(ctx, "orion-compute")
	if err != nil {
		logger.Error("observability init failed", "error", err.Error())
		os.Exit(1)
	}
	defer func() {
		_ = obs.Shutdown(context.Background())
	}()

	dsn := config.String("ORION_DB_DSN", "")
	if dsn == "" {
		logger.Error("ORION_DB_DSN is required")
		os.Exit(1)
	}
	pool, err := kitpg.Connect(ctx, dsn)
	if err != nil {
		logger.Error("postgres connect failed", "error", err.Error())
		os.Exit(1)
	}
	store, err := pgstore.New(ctx, pool)
	if err != nil {
		logger.Error("postgres migrate failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("using postgres store")

	natsURL := config.String("ORION_NATS_URL", "")
	if natsURL == "" {
		logger.Error("ORION_NATS_URL is required")
		os.Exit(1)
	}
	natsClient, err := natsx.New(natsURL)
	if err != nil {
		logger.Error("nats connect failed", "error", err.Error())
		os.Exit(1)
	}
	defer natsClient.Close()
	if err := natsClient.EnsureDefaultStreams(ctx); err != nil {
		logger.Error("ensure NATS streams failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("using NATS JetStream", "url", natsURL)

	cmdPublisher := natsadapter.NewCommandPublisher(natsClient)
	eventPublisher, err := events.NewNATSPublisher(natsURL)
	if err != nil {
		logger.Error("nats events publisher failed", "error", err.Error())
		os.Exit(1)
	}
	defer eventPublisher.Close()

	opClient := operationclient.New(config.String("ORION_OPERATION_GRPC_ADDR", "localhost:50052"))
	defer opClient.Close()

	images := imageclient.New(config.String("ORION_IMAGE_URL", "http://localhost:8085"))
	networks := networkclient.New(config.String("ORION_NETWORK_URL", "http://localhost:8086"))
	volumes := volumeclient.New(config.String("ORION_VOLUME_URL", "http://localhost:8087"))
	placement := placementclient.New(config.String("ORION_PLACEMENT_URL", "http://localhost:8082"))
	exec := nodeagent.New(
		nodeAgentGRPCAddr(),
		config.String("ORION_IMAGE_URL", "http://localhost:8085"),
		placement,
	)

	service := application.NewService(store, eventPublisher, cmdPublisher, opClient, images, networks, volumes, placement, exec)
	handler := httpadapter.NewHandler(logger, service)
	root := http.NewServeMux()
	root.Handle("/metrics", obs.MetricsHandler())
	root.Handle("/", obs.WrapHTTP(handler.Routes()))

	server := &http.Server{
		Addr:              config.String("ORION_COMPUTE_LISTEN_ADDR", ":8083"),
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	reconcileCtx, reconcileCancel := context.WithCancel(ctx)
	defer reconcileCancel()
	startReconcileLoop(logger, service, reconcileCtx, config.Duration("ORION_COMPUTE_RECONCILE_INTERVAL", 30*time.Second))

	go func() {
		<-ctx.Done()
		logger.Info("shutdown signal received, draining...")
		reconcileCancel()

		drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(drainCtx); err != nil {
			logger.Error("server shutdown error", "error", err.Error())
		}
	}()

	logger.Info("starting orion-compute", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("compute server exited", "error", err.Error())
	}
	logger.Info("orion-compute stopped")
}

func nodeAgentGRPCAddr() string {
	if addr := config.String("ORION_NODE_AGENT_GRPC_ADDR", ""); addr != "" {
		return addr
	}

	legacy := config.String("ORION_NODE_AGENT_URL", "localhost:50051")
	if parsed, err := url.Parse(legacy); err == nil && parsed.Hostname() != "" {
		return net.JoinHostPort(parsed.Hostname(), "50051")
	}
	return strings.TrimSpace(legacy)
}

func startReconcileLoop(logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}, service *application.Service, ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("reconcile loop stopping")
				return
			case <-ticker.C:
				reconciled, err := service.Reconcile(ctx)
				if err != nil {
					logger.Error("compute reconcile failed", "error", err.Error())
					continue
				}
				if reconciled > 0 {
					logger.Info("compute reconcile applied", "servers", reconciled)
				}
			}
		}
	}()
}
