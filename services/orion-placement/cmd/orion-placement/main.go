package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	placementv1 "github.com/horizon/orion/gen/go/placement/v1"
	"github.com/horizon/orion/libs/go/kit/config"
	"github.com/horizon/orion/libs/go/kit/logging"
	"github.com/horizon/orion/libs/go/kit/natsx"
	"github.com/horizon/orion/libs/go/kit/natsx/idempotency"
	"github.com/horizon/orion/libs/go/kit/observability"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/libs/go/kit/tlsconfig"
	grpcadapter "github.com/horizon/orion/services/orion-placement/internal/adapters/grpc"
	httpadapter "github.com/horizon/orion/services/orion-placement/internal/adapters/http"
	natsadapter "github.com/horizon/orion/services/orion-placement/internal/adapters/nats"
	pgstore "github.com/horizon/orion/services/orion-placement/internal/adapters/postgres"
	"github.com/horizon/orion/services/orion-placement/internal/application"
	"github.com/horizon/orion/services/orion-placement/internal/domain"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

func main() {
	logger := logging.New("orion-placement")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	obs, err := observability.New(ctx, "orion-placement")
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

	idempotencyManager, err := idempotency.NewManagerForSchema(pool, "orion_placement")
	if err != nil {
		logger.Error("idempotency manager config failed", "error", err.Error())
		os.Exit(1)
	}
	if err := idempotencyManager.InitSchema(ctx); err != nil {
		logger.Error("idempotency schema init failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("idempotency manager initialized")

	natsURL := config.String("ORION_NATS_URL", "nats://localhost:4222")
	natsClient, err := natsx.New(natsURL)
	if err != nil {
		logger.Error("nats connect failed", "error", err.Error())
		os.Exit(1)
	}
	defer natsClient.Close()
	logger.Info("connected to NATS", "url", natsURL)

	if err := natsClient.EnsureDefaultStreams(ctx); err != nil {
		logger.Error("ensure streams failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("NATS streams configured")

	service := application.NewService(store, placementWeights())
	handler := httpadapter.NewHandler(logger, service)
	grpcListener, err := net.Listen("tcp", config.String("ORION_PLACEMENT_GRPC_ADDR", ":50053"))
	if err != nil {
		logger.Error("placement gRPC listen failed", "error", err.Error())
		os.Exit(1)
	}
	grpcOptions := []grpc.ServerOption{grpc.StatsHandler(otelgrpc.NewServerHandler())}
	if transport, tlsErr := tlsconfig.ServerCredentialsFromEnv(); tlsErr != nil {
		logger.Error("TLS configuration invalid", "error", tlsErr.Error())
		os.Exit(1)
	} else if transport != nil {
		grpcOptions = append(grpcOptions, grpc.Creds(transport))
	}
	grpcServer := grpc.NewServer(grpcOptions...)
	placementv1.RegisterPlacementServiceServer(grpcServer, grpcadapter.NewServer(service))

	cmdHandler := natsadapter.NewCommandHandler(service, idempotencyManager)
	sub := natsx.NewSubscriber(natsClient, 1)

	go func() {
		logger.Info("starting NATS command subscriber")
		_ = sub.SubscribeCommand(ctx, "orion.command.placement.>", func(subject string, data []byte, headers map[string][]string) error {
			return cmdHandler.HandlePlacementCommand(ctx, subject, data, headers)
		},
			natsx.WithStream(natsx.StreamORIONCommands),
			natsx.WithDurable("placement-commands"),
		)
	}()
	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil && ctx.Err() == nil {
			logger.Error("placement gRPC server exited", "error", err.Error())
		}
	}()
	root := http.NewServeMux()
	root.Handle("/metrics", obs.MetricsHandler())
	root.Handle("/", obs.WrapHTTP(handler.Routes()))

	server := &http.Server{
		Addr:              config.String("ORION_PLACEMENT_LISTEN_ADDR", ":8082"),
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("starting orion-placement", "addr", server.Addr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("placement server exited", "error", err.Error())
		}
	}()

	<-ctx.Done()
}

func placementWeights() domain.ScoreWeights {
	return domain.ScoreWeights{
		VCPUs:  envFloat("ORION_PLACEMENT_WEIGHT_VCPU", 1),
		Memory: envFloat("ORION_PLACEMENT_WEIGHT_MEMORY", 1.0/1024.0),
		Disk:   envFloat("ORION_PLACEMENT_WEIGHT_DISK", 1),
	}
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(config.String(name, ""), 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
