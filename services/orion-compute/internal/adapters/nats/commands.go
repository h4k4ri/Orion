package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/horizon/orion/libs/go/kit/natsx"
)

type CommandPublisher struct {
	js   jetstream.JetStream
	conn *nats.Conn
}

func NewCommandPublisher(c *natsx.Client) *CommandPublisher {
	return &CommandPublisher{
		js:   c.JS(),
		conn: c.Conn(),
	}
}

type ComputeCommand struct {
	MessageID   string          `json:"message_id"`
	OperationID string          `json:"operation_id"`
	RequestID   string          `json:"request_id"`
	Action      string          `json:"action"`
	Payload     json.RawMessage `json:"payload"`
}

type BuildInstancePayload struct {
	InstanceID string        `json:"instance_id"`
	HostID     string        `json:"host_id"`
	Name       string        `json:"name"`
	VCPUs      int           `json:"vcpus"`
	MemoryMB   int64         `json:"memory_mb"`
	DiskGB     int           `json:"disk_gb"`
	Image      *ImagePayload `json:"image,omitempty"`
	Ports      []PortPayload `json:"ports,omitempty"`
}

type ImagePayload struct {
	ID             string `json:"id"`
	SourcePath     string `json:"source_path,omitempty"`
	SourceURL      string `json:"source_url,omitempty"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
}

type DesiredInstance struct {
	InstanceID      string `json:"instance_id"`
	Name            string `json:"name"`
	VCPUs           int    `json:"vcpus"`
	MemoryMB        int    `json:"memory_mb"`
	DiskGB          int    `json:"disk_gb"`
	ImageID         string `json:"image_id"`
	ImageSourcePath string `json:"image_source_path,omitempty"`
	DesiredState    string `json:"desired_state"`
	Generation      int64  `json:"generation"`
}

type DesiredStateUpdate struct {
	HostID           string                     `json:"host_id"`
	Instances        map[string]DesiredInstance `json:"instances,omitempty"`
	DeletedInstances []string                   `json:"deleted_instances,omitempty"`
	Generation       int64                      `json:"generation"`
}

type PortPayload struct {
	ID         string `json:"id"`
	MACAddress string `json:"mac_address,omitempty"`
	IPAddress  string `json:"ip_address,omitempty"`
	NetworkID  string `json:"network_id"`
}

type DestroyInstancePayload struct {
	InstanceID string `json:"instance_id"`
}

func (p *CommandPublisher) PublishInstanceCreate(ctx context.Context, msgID, operationID, requestID string, payload BuildInstancePayload) error {
	cmd := ComputeCommand{
		MessageID:   msgID,
		OperationID: operationID,
		RequestID:   requestID,
		Action:      "build_instance",
		Payload:     mustMarshal(payload),
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}

	subject := "orion.command.compute.instance.create.v1"
	_, err = p.js.Publish(ctx, subject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("jetstream publish %s: %w", subject, err)
	}
	return nil
}

func (p *CommandPublisher) PublishInstanceDestroy(ctx context.Context, msgID, operationID string, payload DestroyInstancePayload) error {
	cmd := ComputeCommand{
		MessageID:   msgID,
		OperationID: operationID,
		Action:      "destroy_instance",
		Payload:     mustMarshal(payload),
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}

	subject := "orion.command.compute.instance.destroy.v1"
	_, err = p.js.Publish(ctx, subject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("jetstream publish %s: %w", subject, err)
	}
	return nil
}

func (p *CommandPublisher) PublishDesiredState(ctx context.Context, hostID string, generation int64, instance *DesiredInstance, deleted string) error {
	update := DesiredStateUpdate{HostID: hostID, Generation: generation}
	if instance != nil {
		update.Instances = map[string]DesiredInstance{instance.InstanceID: *instance}
	}
	if deleted != "" {
		update.DeletedInstances = []string{deleted}
	}
	data, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("marshal desired state: %w", err)
	}
	msgID := fmt.Sprintf("desired-%s-%d", hostID, generation)
	_, err = p.js.Publish(ctx, "orion.desired.compute."+hostID+".v1", data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("jetstream publish desired state: %w", err)
	}
	return nil
}

func (p *CommandPublisher) PublishInstanceAttachVolume(ctx context.Context, msgID, operationID string, payload map[string]any) error {
	cmd := ComputeCommand{
		MessageID:   msgID,
		OperationID: operationID,
		Action:      "attach_volume",
		Payload:     mustMarshal(payload),
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}

	subject := "orion.command.compute.instance.attach_volume.v1"
	_, err = p.js.Publish(ctx, subject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("jetstream publish %s: %w", subject, err)
	}
	return nil
}

func (p *CommandPublisher) PublishInstanceDetachVolume(ctx context.Context, msgID, operationID string, payload map[string]any) error {
	cmd := ComputeCommand{
		MessageID:   msgID,
		OperationID: operationID,
		Action:      "detach_volume",
		Payload:     mustMarshal(payload),
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}

	subject := "orion.command.compute.instance.detach_volume.v1"
	_, err = p.js.Publish(ctx, subject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("jetstream publish %s: %w", subject, err)
	}
	return nil
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
