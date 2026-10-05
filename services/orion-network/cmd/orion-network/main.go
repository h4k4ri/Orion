package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/horizon/orion/libs/go/kit/config"
	"github.com/horizon/orion/libs/go/kit/events"
	"github.com/horizon/orion/libs/go/kit/logging"
	"github.com/horizon/orion/libs/go/kit/natsx"
	"github.com/horizon/orion/libs/go/kit/natsx/idempotency"
	"github.com/horizon/orion/libs/go/kit/observability"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	httpadapter "github.com/horizon/orion/services/orion-network/internal/adapters/http"
	natsadapter "github.com/horizon/orion/services/orion-network/internal/adapters/nats"
	"github.com/horizon/orion/services/orion-network/internal/adapters/ovn"
	pgstore "github.com/horizon/orion/services/orion-network/internal/adapters/postgres"
	"github.com/horizon/orion/services/orion-network/internal/application"
)

func main() {
	logger := logging.New("orion-network")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	obs, err := observability.New(ctx, "orion-network")
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

	idempotencyManager, err := idempotency.NewManagerForSchema(pool, "orion_network")
	if err != nil {
		logger.Error("idempotency manager config failed", "error", err.Error())
		os.Exit(1)
	}
	if err := idempotencyManager.InitSchema(ctx); err != nil {
		logger.Error("idempotency schema init failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("idempotency manager initialized")

	natsURL := config.String("ORION_NATS_URL", "")
	if natsURL == "" {
		logger.Error("ORION_NATS_URL is required")
		os.Exit(1)
	}

	publisher, err := events.NewNATSPublisher(natsURL)
	if err != nil {
		logger.Error("nats connect failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("using NATS publisher", "url", natsURL)

	natsClient, err := natsx.New(natsURL)
	if err != nil {
		logger.Error("nats jetstream connect failed", "error", err.Error())
		os.Exit(1)
	}
	defer natsClient.Close()
	logger.Info("connected to NATS JetStream", "url", natsURL)

	if err := natsClient.EnsureDefaultStreams(ctx); err != nil {
		logger.Error("ensure streams failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("NATS streams configured")

	driver := ovn.New(config.String("ORION_NETWORK_OVN_NB_DB", ""))
	service := application.NewService(store, driver, publisher)
	handler := httpadapter.NewHandler(logger, service)

	cmdHandler := natsadapter.NewCommandHandler(service, idempotencyManager)
	sub := natsx.NewSubscriber(natsClient, 1)

	go func() {
		logger.Info("starting NATS command subscriber")
		_ = sub.SubscribeCommand(ctx, "orion.command.network.>", func(subject string, data []byte, headers map[string][]string) error {
			return cmdHandler.HandleNetworkCommand(ctx, subject, data, headers)
		},
			natsx.WithStream(natsx.StreamORIONCommands),
			natsx.WithDurable("network-commands"),
		)
	}()

	root := http.NewServeMux()
	root.Handle("/metrics", obs.MetricsHandler())
	root.Handle("/", obs.WrapHTTP(handler.Routes()))

	server := &http.Server{
		Addr:              config.String("ORION_NETWORK_LISTEN_ADDR", ":8086"),
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	startReconcileLoop(logger, service, config.Duration("ORION_NETWORK_RECONCILE_INTERVAL", 15*time.Second))

	logger.Info("starting orion-network", "addr", server.Addr)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()

	<-ctx.Done()
	logger.Info("shutdown signal received, draining...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("network server shutdown error", "error", err.Error())
	}
	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("network server exited", "error", err.Error())
		}
	default:
	}
	cancel()
}

func startReconcileLoop(logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}, service *application.Service, interval time.Duration) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			reconciled, err := service.Reconcile(context.Background())
			if err != nil {
				logger.Error("network reconcile failed", "error", err.Error())
				continue
			}
			if reconciled > 0 {
				logger.Info("network reconcile applied", "ports", reconciled)
			}
		}
	}()
}
