package logging

import (
	"log/slog"
	"os"
	"strings"
)

func New(service string) *slog.Logger {
	level := slog.LevelInfo

	switch strings.ToLower(os.Getenv("ORION_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler).With("service", service)
}
