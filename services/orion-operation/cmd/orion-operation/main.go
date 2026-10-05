package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	operationv1 "github.com/horizon/orion/gen/go/operation/v1"
	"github.com/horizon/orion/libs/go/kit/config"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/logging"
	"github.com/horizon/orion/libs/go/kit/natsx"
	"github.com/horizon/orion/libs/go/kit/observability"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/libs/go/kit/tlsconfig"
	grpcadapter "github.com/horizon/orion/services/orion-operation/internal/adapters/grpc"
	httpadapter "github.com/horizon/orion/services/orion-operation/internal/adapters/http"
	eventnats "github.com/horizon/orion/services/orion-operation/internal/adapters/nats"
	"github.com/horizon/orion/services/orion-operation/internal/adapters/postgres"
	"github.com/horizon/orion/services/orion-operation/internal/application"
	operationports "github.com/horizon/orion/services/orion-operation/internal/ports"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

func main() {
	logger := logging.New("orion-operation")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	obs, err := observability.New(ctx, "orion-operation")
	if err != nil {
		logger.Error("observability init failed", "error", err.Error())
		os.Exit(1)
	}
	defer func() { _ = obs.Shutdown(context.Background()) }()

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
	store, err := postgres.New(ctx, pool)
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

	opSvc := application.NewService(store)
	grpcListener, err := net.Listen("tcp", config.String("ORION_OPERATION_GRPC_ADDR", ":50052"))
	if err != nil {
		logger.Error("operation gRPC listen failed", "error", err.Error())
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
	operationv1.RegisterOperationServiceServer(grpcServer, grpcadapter.NewServer(opSvc))

	eventHandler := eventnats.NewEventHandler(opSvc, natsClient)
	outboxPublisher := natsx.NewPublisher(natsClient)
	go startOutboxDispatcher(ctx, store, outboxPublisher, logger)

	handler := httpadapter.NewHandler(logger, opSvc, pool)

	root := http.NewServeMux()
	root.Handle("/metrics", obs.MetricsHandler())
	root.Handle("/", obs.WrapHTTP(httpx.RequestID(handler.Routes())))

	server := &http.Server{
		Addr:              config.String("ORION_OPERATION_LISTEN_ADDR", ":8090"),
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			logger.Error("operation gRPC server exited", "error", err.Error())
		}
	}()

	go func() {
		if err := eventHandler.Start(ctx); err != nil && ctx.Err() == nil {
			logger.Error("event handler failed", "error", err.Error())
		}
	}()

	go func() {
		<-ctx.Done()
		logger.Info("shutdown signal received, draining...")
		grpcServer.GracefulStop()

		drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(drainCtx); err != nil {
			logger.Error("server shutdown error", "error", err.Error())
		}
	}()

	logger.Info("starting orion-operation", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("operation server exited", "error", err.Error())
	}
	logger.Info("orion-operation stopped")
}

type outboxStore interface {
	ListUnpublishedOutbox(context.Context, int) ([]operationports.OutboxEventRecord, error)
	MarkOutboxPublished(context.Context, string) error
	MarkOutboxFailed(context.Context, operationports.OutboxEventRecord, string) error
	TryAcquireOutboxLeader(context.Context) (operationports.LeaderRelease, bool, error)
}

func startOutboxDispatcher(ctx context.Context, store outboxStore, publisher *natsx.Publisher, logger interface {
	Info(string, ...any)
	Error(string, ...any)
}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			release, leader, err := store.TryAcquireOutboxLeader(ctx)
			if err != nil {
				logger.Error("outbox leader election failed", "error", err.Error())
				continue
			}
			if !leader {
				continue
			}
			events, err := store.ListUnpublishedOutbox(ctx, 100)
			if err != nil {
				release()
				logger.Error("outbox poll failed", "error", err.Error())
				continue
			}
			for _, event := range events {
				subject := event.Subject
				if !strings.HasPrefix(subject, "orion.") {
					subject = "orion.event.operation." + subject + ".v1"
				}
				if err := publisher.PublishWithHeaders(ctx, subject, event.Payload, nats.Header{}, natsx.WithMsgID(event.ID)); err != nil {
					logger.Error("outbox publish failed", "id", event.ID, "error", err.Error())
					if markErr := store.MarkOutboxFailed(ctx, event, err.Error()); markErr != nil {
						logger.Error("outbox failure bookkeeping failed", "id", event.ID, "error", markErr.Error())
					}
					continue
				}
				if err := store.MarkOutboxPublished(ctx, event.ID); err != nil {
					logger.Error("outbox mark failed", "id", event.ID, "error", err.Error())
				}
			}
			release()
		}
	}
}
