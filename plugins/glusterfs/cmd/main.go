package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/horizon/orion/sdk/go/plugin"
)

type glusterConfig struct {
	server    string
	brickRoot string
	bricks    []string
	replica   int
	transport string
}

func main() {
	p := plugin.New(plugin.Config{ID: "glusterfs-plugin", Name: "GlusterFS", Version: "2.0.0", Vendor: "Gluster.org"})
	p.Resource("orion.io/storage.volume", "v1").
		Handle("create", handleVolumeCreate).
		Handle("delete", handleVolumeDelete).
		Handle("attach", handleVolumeAttach).
		Handle("detach", handleVolumeDetach).
		Handle("resize", handleVolumeResize).
		AddCapability("snapshots", true).
		AddCapability("replication", true).
		AddCapability("quota", true).
		Register()
	p.Resource("orion.io/storage.snapshot", "v1").
		Handle("create", handleSnapshotCreate).
		Handle("delete", handleSnapshotDelete).
		Handle("restore", handleSnapshotRestore).
		Register()
	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", handleAttachmentPrepare).
		Handle("release", handleAttachmentRelease).
		Register()

	endpoint := envOr("ORION_PLUGIN_ENDPOINT", ":50053")
	log.Printf("GlusterFS plugin starting on %s", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func handleVolumeCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name string  `json:"name"`
		Size float64 `json:"size"`
		Type string  `json:"type"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.Name); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadGlusterConfig()
	if err != nil {
		return errorResponse("GLUSTER_CONFIG_ERROR", err), nil
	}
	volume := volumeName(input.Name)
	brick := filepath.Join(cfg.brickRoot, volume)
	if err := os.MkdirAll(brick, 0750); err != nil {
		return errorResponse("GLUSTER_CREATE_FAILED", err), nil
	}
	bricks, err := cfg.bricksFor(volume, brick)
	if err != nil {
		return errorResponse("GLUSTER_CONFIG_ERROR", err), nil
	}
	args := []string{"volume", "create", volume}
	if cfg.replica > 1 {
		args = append(args, "replica", strconv.Itoa(cfg.replica))
	}
	args = append(args, "transport", cfg.transport)
	args = append(args, bricks...)
	args = append(args, "force")
	if _, err := runCommand(ctx, "gluster", args...); err != nil {
		return errorResponse("GLUSTER_CREATE_FAILED", err), nil
	}
	if _, err := runCommand(ctx, "gluster", "volume", "start", volume); err != nil {
		return errorResponse("GLUSTER_START_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": volume, "name": input.Name, "size": input.Size, "type": input.Type, "server": cfg.server, "status": "created"}), nil
}

func handleVolumeDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	volume, err := parseVolumeName(input.ID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runCommand(ctx, "gluster", "volume", "stop", volume, "force"); err != nil && !strings.Contains(err.Error(), "not started") {
		return errorResponse("GLUSTER_STOP_FAILED", err), nil
	}
	if _, err := runCommand(ctx, "gluster", "volume", "delete", volume); err != nil {
		return errorResponse("GLUSTER_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"id": volume, "status": "deleted"}), nil
}

func handleVolumeAttach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		VolumeID   string `json:"volume_id"`
		MountPoint string `json:"mount_point"`
		Options    string `json:"mount_options"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if input.MountPoint == "" {
		return errorResponse("INVALID_INPUT", fmt.Errorf("mount_point is required for a real GlusterFS attach")), nil
	}
	cfg, err := loadGlusterConfig()
	if err != nil {
		return errorResponse("GLUSTER_CONFIG_ERROR", err), nil
	}
	volume, err := parseVolumeName(input.VolumeID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := os.MkdirAll(input.MountPoint, 0750); err != nil {
		return errorResponse("GLUSTER_MOUNT_FAILED", err), nil
	}
	args := []string{"-t", "glusterfs"}
	if input.Options != "" {
		args = append(args, "-o", input.Options)
	}
	args = append(args, cfg.server+":"+volume, input.MountPoint)
	if _, err := runCommand(ctx, "mount", args...); err != nil {
		return errorResponse("GLUSTER_MOUNT_FAILED", err), nil
	}
	return success(map[string]string{"volume_id": volume, "mount_point": input.MountPoint, "status": "attached"}), nil
}

func handleVolumeDetach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		MountPoint string `json:"mount_point"`
	}
	if err := decode(req.Payload, &input); err != nil || input.MountPoint == "" {
		if err == nil {
			err = fmt.Errorf("mount_point is required")
		}
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runCommand(ctx, "umount", input.MountPoint); err != nil {
		return errorResponse("GLUSTER_UNMOUNT_FAILED", err), nil
	}
	return success(map[string]string{"mount_point": input.MountPoint, "status": "detached"}), nil
}

func handleVolumeResize(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID   string  `json:"id"`
		Size float64 `json:"size"`
	}
	if err := decode(req.Payload, &input); err != nil || input.Size <= 0 {
		if err == nil {
			err = fmt.Errorf("size must be positive")
		}
		return errorResponse("INVALID_INPUT", err), nil
	}
	volume, err := parseVolumeName(input.ID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	// GlusterFS volumes do not have a block size. This applies a real quota to
	// the volume root, which is the correct filesystem-level resize operation.
	if _, err := runCommand(ctx, "gluster", "volume", "quota", volume, "limit-usage", "/", fmt.Sprintf("%.0fGB", input.Size)); err != nil {
		return errorResponse("GLUSTER_RESIZE_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": volume, "size": input.Size, "status": "available"}), nil
}

func handleSnapshotCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		VolumeID string `json:"volume_id"`
		Name     string `json:"name"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.Name); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	volume, err := parseVolumeName(input.VolumeID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	snapshot := volume + "-" + input.Name
	if _, err := runCommand(ctx, "gluster", "snapshot", "create", snapshot, volume, "no-timestamp"); err != nil {
		return errorResponse("GLUSTER_SNAPSHOT_FAILED", err), nil
	}
	return success(map[string]string{"id": snapshot, "volume_id": volume, "status": "available"}), nil
}

func handleSnapshotDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.ID); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runCommand(ctx, "gluster", "snapshot", "delete", input.ID); err != nil {
		return errorResponse("GLUSTER_SNAPSHOT_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"id": input.ID, "status": "deleted"}), nil
}

func handleSnapshotRestore(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.SnapshotID); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runCommand(ctx, "gluster", "snapshot", "restore", input.SnapshotID); err != nil {
		return errorResponse("GLUSTER_RESTORE_FAILED", err), nil
	}
	return success(map[string]string{"snapshot_id": input.SnapshotID, "status": "restored"}), nil
}

func handleAttachmentPrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	cfg, err := loadGlusterConfig()
	if err != nil {
		return relationshipError("GLUSTER_CONFIG_ERROR", err), nil
	}
	volume, err := parseVolumeName(req.SourceResourceID)
	if err != nil {
		return relationshipError("INVALID_INPUT", err), nil
	}
	if _, err := runCommand(ctx, "gluster", "volume", "info", volume); err != nil {
		return relationshipError("GLUSTER_VOLUME_NOT_FOUND", err), nil
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{
		"protocol": "glusterfs", "server": cfg.server, "volume": volume, "instance_id": req.TargetResourceID,
	})}, nil
}

func handleAttachmentRelease(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	var input struct {
		MountPoint string `json:"mount_point"`
	}
	if len(req.Payload) > 0 {
		_ = decode(req.Payload, &input)
	}
	if input.MountPoint != "" {
		if _, err := runCommand(ctx, "umount", input.MountPoint); err != nil {
			return relationshipError("GLUSTER_UNMOUNT_FAILED", err), nil
		}
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"status": "released"})}, nil
}

func loadGlusterConfig() (glusterConfig, error) {
	server := envOr("ORION_GLUSTER_SERVER", "")
	if server == "" {
		return glusterConfig{}, fmt.Errorf("ORION_GLUSTER_SERVER is required")
	}
	root := envOr("ORION_GLUSTER_BRICK_ROOT", "/var/lib/orion/gluster/bricks")
	if !filepath.IsAbs(root) {
		return glusterConfig{}, fmt.Errorf("ORION_GLUSTER_BRICK_ROOT must be absolute")
	}
	replica := 1
	if value := os.Getenv("ORION_GLUSTER_REPLICA"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return glusterConfig{}, fmt.Errorf("ORION_GLUSTER_REPLICA must be a positive integer")
		}
		replica = parsed
	}
	var bricks []string
	if value := os.Getenv("ORION_GLUSTER_BRICKS"); value != "" {
		for _, brick := range strings.Split(value, ",") {
			brick = strings.TrimSpace(brick)
			if brick != "" {
				bricks = append(bricks, brick)
			}
		}
	}
	return glusterConfig{server: server, brickRoot: filepath.Clean(root), bricks: bricks, replica: replica, transport: envOr("ORION_GLUSTER_TRANSPORT", "tcp")}, nil
}

func (c glusterConfig) bricksFor(volume, defaultBrick string) ([]string, error) {
	if len(c.bricks) == 0 {
		if c.replica > 1 {
			return nil, fmt.Errorf("ORION_GLUSTER_BRICKS must contain at least %d bricks for replica %d", c.replica, c.replica)
		}
		return []string{c.server + ":" + defaultBrick}, nil
	}
	if len(c.bricks) < c.replica {
		return nil, fmt.Errorf("ORION_GLUSTER_BRICKS has %d entries but replica requires %d", len(c.bricks), c.replica)
	}
	result := make([]string, 0, len(c.bricks))
	for _, brick := range c.bricks {
		if !strings.Contains(brick, ":") {
			return nil, fmt.Errorf("invalid Gluster brick %q; expected host:/absolute/path", brick)
		}
		result = append(result, strings.TrimSuffix(brick, "/")+"/"+volume)
	}
	return result, nil
}

func volumeName(name string) string { return "orion_" + name }

func parseVolumeName(id string) (string, error) {
	if strings.HasPrefix(id, "orion_") {
		id = strings.TrimPrefix(id, "orion_")
	}
	if err := validateName(id); err != nil {
		return "", err
	}
	return volumeName(id), nil
}

func validateName(value string) error {
	if value == "" || len(value) > 128 {
		return fmt.Errorf("name is required and must be at most 128 characters")
	}
	for _, r := range value {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-", r)) {
			return fmt.Errorf("invalid name %q", value)
		}
	}
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func success(value interface{}) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: true, Result: marshal(value)}
}

func errorResponse(code string, err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: code, Message: err.Error()}}
}

func relationshipError(code string, err error) *plugin.RelationshipResponse {
	return &plugin.RelationshipResponse{Success: false, Error: &plugin.Error{Code: code, Message: err.Error()}}
}

func marshal(value interface{}) []byte { data, _ := json.Marshal(value); return data }

func decode(data []byte, value interface{}) error { return json.Unmarshal(data, value) }

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
