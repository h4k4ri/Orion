package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/horizon/orion/libs/go/kit/natsx/idempotency"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/services/orion-network/internal/application"
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

type NetworkCommand struct {
	MessageID   string          `json:"message_id"`
	OperationID string          `json:"operation_id"`
	Action      string          `json:"action"`
	Payload     json.RawMessage `json:"payload"`
}

func (h *CommandHandler) HandleNetworkCommand(ctx context.Context, subject string, data []byte, headers map[string][]string) error {
	msgID := ""
	if vals, ok := headers["Nats-Msg-Id"]; ok && len(vals) > 0 {
		msgID = vals[0]
	}

	var cmd NetworkCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return fmt.Errorf("unmarshal command: %w", err)
	}
	if msgID == "" {
		msgID = cmd.MessageID
	}

	processed, err := h.idempotency.CheckAndMark(ctx, msgID, cmd.OperationID, "network")
	if err != nil {
		return fmt.Errorf("idempotency check: %w", err)
	}
	if processed {
		return nil
	}

	switch cmd.Action {
	case "create_network":
		return h.handleCreateNetwork(ctx, cmd.Payload)
	case "create_subnet":
		return h.handleCreateSubnet(ctx, cmd.Payload)
	case "create_port":
		return h.handleCreatePort(ctx, cmd.Payload)
	case "update_port_binding":
		return h.handleUpdatePortBinding(ctx, cmd.Payload)
	default:
		return fmt.Errorf("unknown action: %s", cmd.Action)
	}
}

func (h *CommandHandler) handleCreateNetwork(ctx context.Context, payload json.RawMessage) error {
	var req networkkit.CreateNetworkRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal create network: %w", err)
	}

	_, err := h.service.CreateNetwork(ctx, req)
	return err
}

func (h *CommandHandler) handleCreateSubnet(ctx context.Context, payload json.RawMessage) error {
	var req networkkit.CreateSubnetRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal create subnet: %w", err)
	}

	_, err := h.service.CreateSubnet(ctx, req)
	return err
}

func (h *CommandHandler) handleCreatePort(ctx context.Context, payload json.RawMessage) error {
	var req networkkit.CreatePortRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal create port: %w", err)
	}

	_, err := h.service.CreatePort(ctx, req)
	return err
}

func (h *CommandHandler) handleUpdatePortBinding(ctx context.Context, payload json.RawMessage) error {
	var req struct {
		PortID        string `json:"port_id"`
		BindingHostID string `json:"binding_host_id"`
		BindingStatus string `json:"binding_status"`
		BindingDetail string `json:"binding_detail,omitempty"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal update port binding: %w", err)
	}

	_, err := h.service.UpdatePortBinding(ctx, req.PortID, networkkit.UpdatePortBindingRequest{
		BindingHostID: req.BindingHostID,
		BindingStatus: req.BindingStatus,
		BindingDetail: req.BindingDetail,
	})
	return err
}
