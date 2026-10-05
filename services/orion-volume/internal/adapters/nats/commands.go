package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/horizon/orion/libs/go/kit/natsx/idempotency"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-volume/internal/application"
)

type CommandHandler struct {
	service     *application.Service
	idempotency *idempotency.Manager
}

func NewCommandHandler(service *application.Service, manager *idempotency.Manager) *CommandHandler {
	return &CommandHandler{
		service:     service,
		idempotency: manager,
	}
}

type VolumeCommand struct {
	MessageID   string          `json:"message_id"`
	OperationID string          `json:"operation_id"`
	Action      string          `json:"action"`
	Payload     json.RawMessage `json:"payload"`
}

func (h *CommandHandler) HandleVolumeCommand(ctx context.Context, subject string, data []byte, headers map[string][]string) error {
	msgID := ""
	if vals, ok := headers["Nats-Msg-Id"]; ok && len(vals) > 0 {
		msgID = vals[0]
	}

	var cmd VolumeCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return fmt.Errorf("unmarshal command: %w", err)
	}
	if msgID == "" {
		msgID = cmd.MessageID
	}

	processed, err := h.idempotency.CheckAndMark(ctx, msgID, cmd.OperationID, "volume")
	if err != nil {
		return fmt.Errorf("idempotency check: %w", err)
	}
	if processed {
		return nil
	}

	switch cmd.Action {
	case "create_volume":
		return h.handleCreateVolume(ctx, cmd.Payload)
	case "delete_volume":
		return h.handleDeleteVolume(ctx, cmd.Payload)
	case "attach_volume":
		return h.handleAttachVolume(ctx, cmd.Payload)
	case "detach_volume":
		return h.handleDetachVolume(ctx, cmd.Payload)
	default:
		return fmt.Errorf("unknown action: %s", cmd.Action)
	}
}

func (h *CommandHandler) handleCreateVolume(ctx context.Context, payload json.RawMessage) error {
	var req volumekit.CreateVolumeRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal create volume: %w", err)
	}

	_, err := h.service.CreateVolume(ctx, req)
	return err
}

func (h *CommandHandler) handleDeleteVolume(ctx context.Context, payload json.RawMessage) error {
	var req struct {
		VolumeID string `json:"volume_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal delete volume: %w", err)
	}

	return h.service.DeleteVolume(ctx, req.VolumeID)
}

func (h *CommandHandler) handleAttachVolume(ctx context.Context, payload json.RawMessage) error {
	var req volumekit.AttachVolumeRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal attach volume: %w", err)
	}

	_, err := h.service.AttachVolume(ctx, "", req.ServerID, req.HostID)
	return err
}

func (h *CommandHandler) handleDetachVolume(ctx context.Context, payload json.RawMessage) error {
	var req struct {
		VolumeID string `json:"volume_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal detach volume: %w", err)
	}

	_, err := h.service.DetachVolume(ctx, req.VolumeID)
	return err
}
