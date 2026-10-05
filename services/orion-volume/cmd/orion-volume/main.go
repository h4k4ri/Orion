package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/horizon/orion/libs/go/kit/config"
	"github.com/horizon/orion/libs/go/kit/events"
	"github.com/horizon/orion/libs/go/kit/logging"
	"github.com/horizon/orion/libs/go/kit/natsx"
	"github.com/horizon/orion/libs/go/kit/natsx/idempotency"
	"github.com/horizon/orion/libs/go/kit/observability"
	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	hostagent "github.com/horizon/orion/services/orion-volume/internal/adapters/hostagent"
	httpadapter "github.com/horizon/orion/services/orion-volume/internal/adapters/http"
	natsadapter "github.com/horizon/orion/services/orion-volume/internal/adapters/nats"
	placementclient "github.com/horizon/orion/services/orion-volume/internal/adapters/placementclient"
	pgstore "github.com/horizon/orion/services/orion-volume/internal/adapters/postgres"
	"github.com/horizon/orion/services/orion-volume/internal/application"
	"github.com/horizon/orion/services/orion-volume/internal/ports"
)

func main() {
	logger := logging.New("orion-volume")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	obs, err := observability.New(ctx, "orion-volume")
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

	idempotencyManager, err := idempotency.NewManagerForSchema(pool, "orion_volume")
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

	placement := placementclient.New(config.String("ORION_PLACEMENT_URL", "http://localhost:8082"))
	agent := hostagent.New(config.String("ORION_VOLUME_HOST_AGENT_URL", "grpc://localhost:50055"), placement)
	service := application.NewService(
		store,
		agent,
		placement,
		publisher,
		config.String("ORION_VOLUME_CELL_ID", "cell_local"),
	)
	handler := httpadapter.NewHandler(logger, service)

	cmdHandler := natsadapter.NewCommandHandler(service, idempotencyManager)
	sub := natsx.NewSubscriber(natsClient, 1)

	go func() {
		logger.Info("starting NATS command subscriber")
		_ = sub.SubscribeCommand(ctx, "orion.command.volume.>", func(subject string, data []byte, headers map[string][]string) error {
			return cmdHandler.HandleVolumeCommand(ctx, subject, data, headers)
		},
			natsx.WithStream(natsx.StreamORIONCommands),
			natsx.WithDurable("volume-commands"),
		)
	}()

	root := http.NewServeMux()
	root.Handle("/metrics", obs.MetricsHandler())
	root.Handle("/", obs.WrapHTTP(handler.Routes()))

	server := &http.Server{
		Addr:              config.String("ORION_VOLUME_LISTEN_ADDR", ":8087"),
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	startReconcileLoop(logger, service, config.Duration("ORION_VOLUME_RECONCILE_INTERVAL", 30*time.Second))

	logger.Info("starting orion-volume", "addr", server.Addr)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()

	<-ctx.Done()
	logger.Info("shutdown signal received, draining...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("volume server shutdown error", "error", err.Error())
	}
	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("volume server exited", "error", err.Error())
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
				logger.Error("volume reconcile failed", "error", err.Error())
				continue
			}
			if reconciled > 0 {
				logger.Info("volume reconcile applied", "volumes", reconciled)
			}
		}
	}()
}

// memoryVolumeStore is a legacy in-memory implementation kept for isolated tests and tooling.
type memoryVolumeStore struct {
	mu        sync.RWMutex
	items     map[string]volumekit.Volume
	snapshots map[string]volumekit.VolumeSnapshot
}

func (m *memoryVolumeStore) SaveVolume(_ context.Context, v volumekit.Volume) error {
	m.mu.Lock()
	m.items[v.ID] = v
	m.mu.Unlock()
	return nil
}

func (m *memoryVolumeStore) GetVolume(_ context.Context, id string) (volumekit.Volume, error) {
	m.mu.RLock()
	v, ok := m.items[id]
	m.mu.RUnlock()
	if !ok {
		return volumekit.Volume{}, ports.ErrVolumeRecordNotFound
	}
	return v, nil
}

func (m *memoryVolumeStore) ListVolumes(_ context.Context) ([]volumekit.Volume, error) {
	m.mu.RLock()
	items := make([]volumekit.Volume, 0, len(m.items))
	for _, v := range m.items {
		items = append(items, v)
	}
	m.mu.RUnlock()
	return items, nil
}

func (m *memoryVolumeStore) DeleteVolume(_ context.Context, id string) error {
	m.mu.Lock()
	delete(m.items, id)
	m.mu.Unlock()
	return nil
}

func (m *memoryVolumeStore) SaveSnapshot(_ context.Context, item volumekit.VolumeSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshots == nil {
		m.snapshots = map[string]volumekit.VolumeSnapshot{}
	}
	m.snapshots[item.ID] = item
	return nil
}
func (m *memoryVolumeStore) GetSnapshot(_ context.Context, id string) (volumekit.VolumeSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.snapshots[id]
	if !ok {
		return volumekit.VolumeSnapshot{}, ports.ErrSnapshotRecordNotFound
	}
	return item, nil
}
func (m *memoryVolumeStore) ListSnapshots(_ context.Context, projectID, volumeID string) ([]volumekit.VolumeSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]volumekit.VolumeSnapshot, 0)
	for _, item := range m.snapshots {
		if (projectID == "" || item.ProjectID == projectID) && (volumeID == "" || item.VolumeID == volumeID) {
			items = append(items, item)
		}
	}
	return items, nil
}
func (m *memoryVolumeStore) DeleteSnapshot(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.snapshots, id)
	return nil
}
