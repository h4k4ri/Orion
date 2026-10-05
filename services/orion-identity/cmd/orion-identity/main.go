package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/horizon/orion/libs/go/kit/config"
	"github.com/horizon/orion/libs/go/kit/logging"
	"github.com/horizon/orion/libs/go/kit/natsx"
	"github.com/horizon/orion/libs/go/kit/observability"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/services/orion-identity/internal/adapters/catalog"
	httpadapter "github.com/horizon/orion/services/orion-identity/internal/adapters/http"
	pgstore "github.com/horizon/orion/services/orion-identity/internal/adapters/postgres"
	identityapp "github.com/horizon/orion/services/orion-identity/internal/application"
	"github.com/nats-io/nats.go"
)

func main() {
	logger := logging.New("orion-identity")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	obs, err := observability.New(ctx, "orion-identity")
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

	if natsURL := config.String("ORION_NATS_URL", ""); natsURL != "" {
		natsClient, natsErr := natsx.New(natsURL)
		if natsErr != nil {
			logger.Error("audit NATS connect failed", "error", natsErr.Error())
			os.Exit(1)
		}
		defer natsClient.Close()
		if natsErr := natsClient.EnsureDefaultStreams(ctx); natsErr != nil {
			logger.Error("audit NATS streams failed", "error", natsErr.Error())
			os.Exit(1)
		}
		go startAuditConsumer(ctx, store, natsClient, logger)
	} else {
		logger.Warn("ORION_NATS_URL is empty; audit event consumer disabled")
	}

	catalogProvider := catalog.NewStatic()
	service := identityapp.NewService(
		store,
		catalogProvider,
		config.Duration("ORION_IDENTITY_TOKEN_TTL", time.Hour),
	)

	handler := httpadapter.NewHandler(logger, service)
	root := http.NewServeMux()
	root.Handle("/metrics", obs.MetricsHandler())
	root.Handle("/", obs.WrapHTTP(handler.Routes()))
	server := &http.Server{
		Addr:              config.String("ORION_IDENTITY_LISTEN_ADDR", ":8081"),
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("starting orion-identity", "addr", server.Addr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("identity server exited", "error", err.Error())
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("identity server shutdown error", "error", err.Error())
	}
}

func startAuditConsumer(ctx context.Context, store *pgstore.Store, client *natsx.Client, logger interface {
	Info(string, ...any)
	Error(string, ...any)
}) {
	subscriber := natsx.NewSubscriber(client, 1)
	err := subscriber.Consume(ctx, "orion.event.>", func(msg *nats.Msg) error {
		var envelope map[string]any
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			return err
		}
		record := pgstore.AuditRecord{
			Operation:    stringField(envelope, "event_type", msg.Subject),
			ResourceType: stringField(envelope, "resource_type", "event"),
			ResourceID:   stringField(envelope, "resource_id", stringField(envelope, "operation_id", "")),
			ProjectID:    stringField(envelope, "project_id", ""),
			RequestID:    stringField(envelope, "request_id", ""),
			OperationID:  stringField(envelope, "operation_id", ""),
			Result:       "accepted",
			Metadata:     msg.Data,
		}
		if task, ok := envelope["task"].(map[string]any); ok {
			record.ActorID = stringField(task, "requested_by", "")
			record.RequestID = stringField(task, "request_id", record.RequestID)
			if record.ResourceID == "" {
				record.ResourceID = stringField(task, "target_ref", "")
			}
		}
		if err := store.AppendAudit(ctx, record); err != nil {
			return err
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		logger.Error("audit consumer stopped", "error", err.Error())
	}
}

func stringField(values map[string]any, key, fallback string) string {
	value, ok := values[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
