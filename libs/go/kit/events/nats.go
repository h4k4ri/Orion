package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/natsx"
)

// NATSPublisher publishes task events to NATS JetStream-compatible subjects.
type NATSPublisher struct {
	client    *natsx.Client
	publisher *natsx.Publisher
}

// NewNATSPublisher connects to NATS and returns a ready publisher.
func NewNATSPublisher(url string) (*NATSPublisher, error) {
	client, err := natsx.New(url, natsx.WithRetry(-1, 2*time.Second))
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.EnsureDefaultStreams(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("ensure event streams: %w", err)
	}
	return &NATSPublisher{client: client, publisher: natsx.NewPublisher(client)}, nil
}

// PublishTask serialises the event and publishes it to the versioned event stream.
func (p *NATSPublisher) PublishTask(ctx context.Context, event TaskEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal task event: %w", err)
	}
	return p.publisher.Publish(ctx, taskSubject(event), data, natsx.WithMsgID(event.Task.ID+":"+event.EventType))
}

func (p *NATSPublisher) PublishResource(ctx context.Context, event ResourceEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal resource event: %w", err)
	}
	subject := resourceSubject(event)
	return p.publisher.Publish(ctx, subject, data, natsx.WithMsgID(event.ResourceID+":"+event.EventType))
}

// Close drains and closes the underlying NATS connection.
func (p *NATSPublisher) Close() {
	p.client.Close()
}

func taskSubject(event TaskEvent) string {
	parts := strings.Split(event.Task.Kind, ".")
	domain := "compute"
	resource := "instance"
	if len(parts) > 0 {
		switch parts[0] {
		case "network":
			domain, resource = "network", "resource"
		case "volume":
			domain, resource = "volume", "resource"
		case "server":
			domain, resource = "compute", "instance"
		}
	}
	verb := "updated"
	switch event.EventType {
	case TaskCreated:
		verb = "created"
	case TaskStarted:
		verb = "started"
	case TaskSucceeded:
		verb = "created"
	case TaskFailed:
		verb = "failed"
	}
	return fmt.Sprintf("orion.event.%s.%s.%s.v1", domain, resource, verb)
}

func resourceSubject(event ResourceEvent) string {
	domain := "network"
	resource := event.ResourceType
	if resource == "volume" {
		domain = "volume"
	}

	parts := strings.Split(event.EventType, ".")
	verb := event.EventType
	if len(parts) > 1 {
		verb = strings.Join(parts[1:], ".")
		if resource == "" {
			resource = parts[0]
		}
		if parts[0] == "volume" {
			domain = "volume"
		}
	}
	if resource == "" {
		resource = "resource"
	}
	if verb == "" {
		verb = "updated"
	}
	return fmt.Sprintf("orion.event.%s.%s.%s.v1", domain, resource, verb)
}
