package nats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/horizon/orion/libs/go/kit/natsx/idempotency"
	"github.com/horizon/orion/services/orion-placement/internal/application"
	"github.com/horizon/orion/services/orion-placement/internal/domain"
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

type PlacementCommand struct {
	MessageID   string          `json:"message_id"`
	OperationID string          `json:"operation_id"`
	Action      string          `json:"action"`
	Payload     json.RawMessage `json:"payload"`
}

type RegisterHostPayload struct {
	HostID   string   `json:"host_id"`
	CellID   string   `json:"cell_id"`
	Group    string   `json:"group"`
	Traits   []string `json:"traits"`
	VCPUs    int      `json:"vcpus"`
	MemoryMB int      `json:"memory_mb"`
	DiskGB   int      `json:"disk_gb"`
}

type SelectHostPayload struct {
	CellID                 string   `json:"cell_id"`
	VCPUs                  int      `json:"vcpus"`
	MemoryMB               int      `json:"memory_mb"`
	DiskGB                 int      `json:"disk_gb"`
	TraitsRequired         []string `json:"traits_required"`
	RequireNodeAgent       bool     `json:"require_node_agent"`
	RequireVolumeHostAgent bool     `json:"require_volume_host_agent"`
}

type ReleaseHostPayload struct {
	HostID   string `json:"host_id"`
	VCPUs    int    `json:"vcpus"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
}

func (h *CommandHandler) HandlePlacementCommand(ctx context.Context, subject string, data []byte, headers map[string][]string) error {
	msgID := ""
	if vals, ok := headers["Nats-Msg-Id"]; ok && len(vals) > 0 {
		msgID = vals[0]
	}

	var cmd PlacementCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return fmt.Errorf("unmarshal command: %w", err)
	}
	if msgID == "" {
		msgID = cmd.MessageID
	}

	processed, err := h.idempotency.CheckAndMark(ctx, msgID, cmd.OperationID, "placement")
	if err != nil {
		return fmt.Errorf("idempotency check: %w", err)
	}
	if processed {
		return nil
	}

	switch cmd.Action {
	case "register_host":
		return h.handleRegisterHost(ctx, cmd.Payload)
	case "select_host":
		return h.handleSelectHost(ctx, cmd.Payload)
	case "release_host":
		return h.handleReleaseHost(ctx, cmd.Payload)
	default:
		return fmt.Errorf("unknown action: %s", cmd.Action)
	}
}

func (h *CommandHandler) handleRegisterHost(ctx context.Context, payload json.RawMessage) error {
	var req RegisterHostPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal register host: %w", err)
	}

	h.service.RegisterHost(ctx, domain.RegisterHostRequest{
		HostID:   req.HostID,
		CellID:   req.CellID,
		Group:    req.Group,
		Traits:   req.Traits,
		VCPUs:    req.VCPUs,
		MemoryMB: req.MemoryMB,
		DiskGB:   req.DiskGB,
	})
	return nil
}

func (h *CommandHandler) handleSelectHost(ctx context.Context, payload json.RawMessage) error {
	var req SelectHostPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal select host: %w", err)
	}

	_, err := h.service.SelectHost(ctx, domain.SelectHostRequest{
		CellID:                 req.CellID,
		VCPUs:                  req.VCPUs,
		MemoryMB:               req.MemoryMB,
		DiskGB:                 req.DiskGB,
		TraitsRequired:         req.TraitsRequired,
		RequireNodeAgent:       req.RequireNodeAgent,
		RequireVolumeHostAgent: req.RequireVolumeHostAgent,
	})
	return err
}

func (h *CommandHandler) handleReleaseHost(ctx context.Context, payload json.RawMessage) error {
	var req ReleaseHostPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("unmarshal release host: %w", err)
	}

	return h.service.ReleaseHost(ctx, domain.ReleaseHostRequest{
		HostID:   req.HostID,
		VCPUs:    req.VCPUs,
		MemoryMB: req.MemoryMB,
		DiskGB:   req.DiskGB,
	})
}
