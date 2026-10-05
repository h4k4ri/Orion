package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/horizon/orion/libs/go/kit/config"
	"github.com/horizon/orion/libs/go/kit/logging"
	"github.com/horizon/orion/libs/go/kit/observability"
	computeclient "github.com/horizon/orion/services/orion-api/internal/adapters/computeclient"
	httpadapter "github.com/horizon/orion/services/orion-api/internal/adapters/http"
	identityclient "github.com/horizon/orion/services/orion-api/internal/adapters/identityclient"
	operationclient "github.com/horizon/orion/services/orion-api/internal/adapters/operationclient"
	volumeclient "github.com/horizon/orion/services/orion-api/internal/adapters/volumeclient"
	"github.com/horizon/orion/services/orion-api/internal/application"
)

func main() {
	logger := logging.New("orion-api")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	obs, err := observability.New(ctx, "orion-api")
	if err != nil {
		logger.Error("observability init failed", "error", err.Error())
		return
	}
	defer func() {
		_ = obs.Shutdown(context.Background())
	}()

	compute := computeclient.New(config.String("ORION_COMPUTE_URL", "http://localhost:8083"))
	volume := volumeclient.New(config.String("ORION_VOLUME_URL", "http://localhost:8087"))
	operation := operationclient.New(config.String("ORION_OPERATION_GRPC_ADDR", "localhost:50052"))
	defer operation.Close()
	service := application.NewService(compute, volume, operation)
	tokenValidator := identityclient.New(config.String("ORION_IDENTITY_URL", "http://localhost:8081"))
	handler := httpadapter.NewHandler(logger, service, tokenValidator)
	root := http.NewServeMux()
	root.Handle("/metrics", obs.MetricsHandler())
	root.Handle("/", obs.WrapHTTP(handler.Routes()))

	server := &http.Server{
		Addr:              config.String("ORION_API_LISTEN_ADDR", ":8080"),
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("starting orion-api", "addr", server.Addr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("api server exited", "error", err.Error())
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("api server shutdown error", "error", err.Error())
	}
	_ = os.Stdout.Sync()
}
