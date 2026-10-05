package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"

	"github.com/horizon/orion/sdk/go/plugin"
)

type cephConfig struct {
	pool      string
	namespace string
	conf      string
	user      string
	keyring   string
}

func main() {
	p := plugin.New(plugin.Config{ID: "ceph-plugin", Name: "Ceph", Version: "2.0.0", Vendor: "Ceph.io"})

	p.Resource("orion.io/storage.volume", "v1").
		Handle("create", handleVolumeCreate).
		Handle("delete", handleVolumeDelete).
		Handle("attach", handleVolumeAttach).
		Handle("detach", handleVolumeDetach).
		Handle("resize", handleVolumeResize).
		AddCapability("snapshots", true).
		AddCapability("clones", true).
		AddCapability("thin_provisioning", true).
		Register()

	p.Resource("orion.io/storage.snapshot", "v1").
		Handle("create", handleSnapshotCreate).
		Handle("delete", handleSnapshotDelete).
		Handle("clone", handleSnapshotClone).
		Handle("rollback", handleSnapshotRollback).
		Register()

	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", handleAttachmentPrepare).
		Handle("release", handleAttachmentRelease).
		Register()

	endpoint := envOr("ORION_PLUGIN_ENDPOINT", ":50052")
	log.Printf("Ceph plugin starting on %s", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func handleVolumeCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name string  `json:"name"`
		Size float64 `json:"size"`
		Pool string  `json:"pool"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.Name); err != nil || input.Size <= 0 {
		if err == nil {
			err = fmt.Errorf("size must be positive")
		}
		return errorResponse("INVALID_INPUT", err), nil
	}

	cfg := loadCephConfig(input.Pool)
	image := input.Name
	imageID := cfg.pool + "/" + image
	if _, err := runRBD(ctx, cfg, "create", "--image-feature", "layering", "--image-format", "2", "--size", sizeMiB(input.Size), image); err != nil {
		return errorResponse("CEPH_CREATE_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": imageID, "name": image, "pool": cfg.pool, "size": input.Size, "status": "available"}), nil
}

func handleVolumeDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, image, err := parseImageID(input.ID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runRBD(ctx, cfg, "rm", image); err != nil {
		return errorResponse("CEPH_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"id": cfg.pool + "/" + image, "status": "deleted"}), nil
}

func handleVolumeAttach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		VolumeID   string `json:"volume_id"`
		MountPoint string `json:"mount_point"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, image, err := parseImageID(input.VolumeID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	device, err := runRBD(ctx, cfg, "map", image)
	if err != nil {
		return errorResponse("CEPH_MAP_FAILED", err), nil
	}
	result := map[string]interface{}{"volume_id": cfg.pool + "/" + image, "device": strings.TrimSpace(device), "status": "attached"}
	if input.MountPoint != "" {
		if err := runMount(ctx, strings.TrimSpace(device), input.MountPoint, ""); err != nil {
			return errorResponse("CEPH_MOUNT_FAILED", err), nil
		}
		result["mount_point"] = input.MountPoint
	}
	return success(result), nil
}

func handleVolumeDetach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Device     string `json:"device"`
		MountPoint string `json:"mount_point"`
		VolumeID   string `json:"volume_id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if input.MountPoint != "" {
		if _, err := runCommand(ctx, "umount", input.MountPoint); err != nil {
			return errorResponse("CEPH_UNMOUNT_FAILED", err), nil
		}
	}
	cfg, image, err := parseImageID(input.VolumeID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	target := input.Device
	if target == "" {
		target = image
	}
	if _, err := runRBD(ctx, cfg, "unmap", target); err != nil {
		return errorResponse("CEPH_UNMAP_FAILED", err), nil
	}
	return success(map[string]string{"status": "detached"}), nil
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
	cfg, image, err := parseImageID(input.ID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runRBD(ctx, cfg, "resize", "--size", sizeMiB(input.Size), image); err != nil {
		return errorResponse("CEPH_RESIZE_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": cfg.pool + "/" + image, "size": input.Size, "status": "available"}), nil
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
	cfg, image, err := parseImageID(input.VolumeID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	snapshotID := image + "@" + input.Name
	if _, err := runRBD(ctx, cfg, "snap", "create", snapshotID); err != nil {
		return errorResponse("CEPH_SNAPSHOT_FAILED", err), nil
	}
	return success(map[string]string{"id": cfg.pool + "/" + snapshotID, "volume_id": cfg.pool + "/" + image, "status": "available"}), nil
}

func handleSnapshotDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, snapshot, err := parseImageID(input.ID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runRBD(ctx, cfg, "snap", "rm", snapshot); err != nil {
		return errorResponse("CEPH_SNAPSHOT_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"id": input.ID, "status": "deleted"}), nil
}

func handleSnapshotClone(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		SnapshotID string `json:"snapshot_id"`
		CloneName  string `json:"clone_name"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.CloneName); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, snapshot, err := parseImageID(input.SnapshotID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cloneID := cfg.pool + "/" + input.CloneName
	if _, err := runRBD(ctx, cfg, "clone", snapshot, input.CloneName); err != nil {
		return errorResponse("CEPH_CLONE_FAILED", err), nil
	}
	return success(map[string]string{"id": cloneID, "source_snapshot": input.SnapshotID, "status": "available"}), nil
}

func handleSnapshotRollback(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, snapshot, err := parseImageID(input.SnapshotID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runRBD(ctx, cfg, "snap", "rollback", snapshot); err != nil {
		return errorResponse("CEPH_ROLLBACK_FAILED", err), nil
	}
	return success(map[string]string{"snapshot_id": input.SnapshotID, "status": "rolled_back"}), nil
}

func handleAttachmentPrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	cfg := loadCephConfig("")
	if _, _, err := parseImageID(req.SourceResourceID); err != nil {
		return &plugin.RelationshipResponse{Success: false, Error: &plugin.Error{Code: "INVALID_INPUT", Message: err.Error()}}, nil
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]interface{}{
		"protocol": "rbd", "pool": cfg.pool, "volume_id": req.SourceResourceID, "instance_id": req.TargetResourceID,
	})}, nil
}

func handleAttachmentRelease(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	var input struct {
		Device string `json:"device"`
	}
	if len(req.Payload) > 0 {
		_ = decode(req.Payload, &input)
	}
	if input.Device != "" {
		cfg := loadCephConfig("")
		if _, err := runRBD(ctx, cfg, "unmap", input.Device); err != nil {
			return &plugin.RelationshipResponse{Success: false, Error: &plugin.Error{Code: "CEPH_UNMAP_FAILED", Message: err.Error()}}, nil
		}
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"status": "released"})}, nil
}

func loadCephConfig(poolOverride string) cephConfig {
	pool := poolOverride
	if pool == "" {
		pool = envOr("ORION_CEPH_POOL", "rbd")
	}
	return cephConfig{pool: pool, namespace: os.Getenv("ORION_CEPH_NAMESPACE"), conf: os.Getenv("ORION_CEPH_CONF"), user: os.Getenv("ORION_CEPH_USER"), keyring: os.Getenv("ORION_CEPH_KEYRING")}
}

func runRBD(ctx context.Context, cfg cephConfig, args ...string) (string, error) {
	global := []string{}
	if cfg.conf != "" {
		global = append(global, "--conf", cfg.conf)
	}
	if cfg.user != "" {
		global = append(global, "--id", cfg.user)
	}
	if cfg.keyring != "" {
		global = append(global, "--keyring", cfg.keyring)
	}
	if cfg.pool != "" {
		global = append(global, "--pool", cfg.pool)
	}
	if cfg.namespace != "" {
		global = append(global, "--namespace", cfg.namespace)
	}
	return runCommand(ctx, "rbd", append(global, args...)...)
}

func parseImageID(id string) (cephConfig, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) == 1 {
		if err := validateName(parts[0]); err != nil {
			return cephConfig{}, "", err
		}
		return loadCephConfig(""), parts[0], nil
	}
	if len(parts) != 2 || parts[0] == "" {
		return cephConfig{}, "", fmt.Errorf("image id must be pool/name")
	}
	if err := validateName(parts[0]); err != nil {
		return cephConfig{}, "", err
	}
	if err := validateName(parts[1]); err != nil {
		return cephConfig{}, "", err
	}
	return loadCephConfig(parts[0]), parts[1], nil
}

func sizeMiB(size float64) string { return strconv.FormatInt(int64(math.Ceil(size*1024)), 10) }

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

func runMount(ctx context.Context, device, mountPoint, options string) error {
	if err := os.MkdirAll(mountPoint, 0750); err != nil {
		return err
	}
	args := []string{}
	if options != "" {
		args = append(args, "-o", options)
	}
	_, err := runCommand(ctx, "mount", append(args, device, mountPoint)...)
	return err
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
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

func marshal(value interface{}) []byte { data, _ := json.Marshal(value); return data }

func decode(data []byte, value interface{}) error { return json.Unmarshal(data, value) }

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
