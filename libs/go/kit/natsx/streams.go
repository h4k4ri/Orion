package natsx

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

type StreamConfig struct {
	Name     string
	Subjects []string
	MaxAge   time.Duration
	MaxBytes int64
	Storage  jetstream.StorageType
	Replicas int
}

func (c *Client) EnsureStream(ctx context.Context, cfg StreamConfig) error {
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 7 * 24 * time.Hour
	}
	if cfg.Storage == 0 {
		cfg.Storage = jetstream.FileStorage
	}
	if cfg.Replicas == 0 {
		cfg.Replicas = 1
	}

	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     cfg.Name,
		Subjects: cfg.Subjects,
		MaxAge:   cfg.MaxAge,
		MaxBytes: cfg.MaxBytes,
		Storage:  cfg.Storage,
		Replicas: cfg.Replicas,
	})
	if err != nil {
		return fmt.Errorf("stream create/update %s: %w", cfg.Name, err)
	}

	return nil
}

const (
	StreamORIONCommands = "ORION_COMMANDS"
	StreamORIONEvents   = "ORION_EVENTS"
	StreamORIONDesired  = "ORION_DESIRED_STATE"

	SubjectCommandCompute   = "orion.command.compute.>"
	SubjectEventCompute     = "orion.event.compute.>"
	SubjectCommandNetwork   = "orion.command.network.>"
	SubjectEventNetwork     = "orion.event.network.>"
	SubjectCommandVolume    = "orion.command.volume.>"
	SubjectEventVolume      = "orion.event.volume.>"
	SubjectCommandImage     = "orion.command.image.>"
	SubjectEventImage       = "orion.event.image.>"
	SubjectCommandPlacement = "orion.command.placement.>"
	SubjectEventPlacement   = "orion.event.placement.>"
	SubjectDLQ               = "orion.dlq.>"
)

func DefaultStreamConfigs() []StreamConfig {
	return []StreamConfig{
		{
			Name:     StreamORIONCommands,
			Subjects: []string{SubjectCommandCompute, SubjectCommandNetwork, SubjectCommandVolume, SubjectCommandImage, SubjectCommandPlacement},
			MaxAge:   7 * 24 * time.Hour,
			Storage:  jetstream.FileStorage,
			Replicas: 1,
		},
		{
			Name:     StreamORIONEvents,
			Subjects: []string{SubjectEventCompute, SubjectEventNetwork, SubjectEventVolume, SubjectEventImage, SubjectEventPlacement},
			MaxAge:   30 * 24 * time.Hour,
			Storage:  jetstream.FileStorage,
			Replicas: 1,
		},
		{
			Name:     StreamORIONDesired,
			Subjects: []string{"orion.desired.compute.>", "orion.desired.volume.>"},
			MaxAge:   30 * 24 * time.Hour,
			Storage:  jetstream.FileStorage,
			Replicas: 1,
		},
	}
}

func (c *Client) EnsureDefaultStreams(ctx context.Context) error {
	for _, cfg := range DefaultStreamConfigs() {
		if err := c.EnsureStream(ctx, cfg); err != nil {
			return err
		}
	}
	return nil
}
