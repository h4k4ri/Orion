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
	httpadapter "github.com/horizon/orion/services/orion-image/internal/adapters/http"
	"github.com/horizon/orion/services/orion-image/internal/application"
)

func main() {
	logger := logging.New("orion-image")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	obs, err := observability.New(ctx, "orion-image")
	if err != nil {
		logger.Error("observability init failed", "error", err.Error())
		return
	}
	defer func() {
		_ = obs.Shutdown(context.Background())
	}()

	service, err := application.NewService(config.String("ORION_IMAGE_STORE_DIR", "/var/tmp/orion-image"))
	if err != nil {
		logger.Error("failed to initialize image service", "error", err.Error())
		return
	}

	handler := httpadapter.NewHandler(logger, service)
	root := http.NewServeMux()
	root.Handle("/metrics", obs.MetricsHandler())
	root.Handle("/", obs.WrapHTTP(handler.Routes()))
	server := &http.Server{
		Addr:              config.String("ORION_IMAGE_LISTEN_ADDR", ":8085"),
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("starting orion-image", "addr", server.Addr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("image server exited", "error", err.Error())
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("image server shutdown error", "error", err.Error())
	}
	_ = os.Stdout.Sync()
}
