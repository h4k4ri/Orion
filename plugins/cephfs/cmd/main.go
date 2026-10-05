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

type cephFSConfig struct {
	name      string
	monitors  string
	user      string
	conf      string
	keyring   string
	group     string
	mountRoot string
}

func main() {
	p := plugin.New(plugin.Config{ID: "cephfs-plugin", Name: "CephFS", Version: "1.0.0", Vendor: "Ceph.io"})
	p.Resource("orion.io/storage.filesystem", "v1").
		Handle("create", handleCreate).
		Handle("delete", handleDelete).
		Handle("resize", handleResize).
		Handle("mount", handleMount).
		Handle("unmount", handleUnmount).
		AddCapability("quotas", true).
		AddCapability("concurrent_access", true).
		Register()
	p.Resource("orion.io/storage.share", "v1").
		Handle("create", handleCreate).
		Handle("delete", handleDelete).
		Handle("attach", handleMount).
		Handle("detach", handleUnmount).
		Handle("create_snapshot", handleSnapshotCreate).
		Handle("delete_snapshot", handleSnapshotDelete).
		Handle("restore_snapshot", handleSnapshotRestore).
		AddCapability("quotas", true).
		AddCapability("snapshots", true).
		AddCapability("concurrent_access", true).
		Register()
	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", handlePrepare).
		Handle("release", handleRelease).
		Register()

	endpoint := envOr("ORION_PLUGIN_ENDPOINT", ":50056")
	log.Printf("CephFS plugin starting on %s", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func handleSnapshotCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID       string `json:"id"`
		ShareID  string `json:"share_id"`
		Snapshot string `json:"snapshot"`
		Name     string `json:"name"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	id := firstNonEmpty(input.ID, input.ShareID)
	cfg, subvolume, err := parseID(id)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	snapshot := firstNonEmpty(input.Snapshot, input.Name)
	if err := validateName(snapshot); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	args := []string{"fs", "subvolume", "snapshot", "create", cfg.name, subvolume, snapshot}
	if cfg.group != "" {
		args = append(args, "--group", cfg.group)
	}
	if _, err := runCeph(ctx, cfg, args...); err != nil {
		return errorResponse("CEPHFS_SNAPSHOT_CREATE_FAILED", err), nil
	}
	return success(map[string]string{"id": id + "@" + snapshot, "share_id": id, "snapshot": snapshot, "status": "available"}), nil
}

func handleSnapshotDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID       string `json:"id"`
		ShareID  string `json:"share_id"`
		Snapshot string `json:"snapshot"`
		Name     string `json:"name"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	id := firstNonEmpty(input.ID, input.ShareID)
	cfg, subvolume, err := parseID(id)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	snapshot := firstNonEmpty(input.Snapshot, input.Name)
	if err := validateName(snapshot); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	args := []string{"fs", "subvolume", "snapshot", "rm", cfg.name, subvolume, snapshot}
	if cfg.group != "" {
		args = append(args, "--group", cfg.group)
	}
	if _, err := runCeph(ctx, cfg, args...); err != nil {
		return errorResponse("CEPHFS_SNAPSHOT_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"share_id": id, "snapshot": snapshot, "status": "deleted"}), nil
}

func handleSnapshotRestore(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID       string `json:"id"`
		ShareID  string `json:"share_id"`
		Snapshot string `json:"snapshot"`
		Name     string `json:"name"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	id := firstNonEmpty(input.ID, input.ShareID)
	cfg, subvolume, err := parseID(id)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	snapshot := firstNonEmpty(input.Snapshot, input.Name)
	if err := validateName(snapshot); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	args := []string{"fs", "subvolume", "snapshot", "rollback", cfg.name, subvolume, snapshot}
	if cfg.group != "" {
		args = append(args, "--group", cfg.group)
	}
	if _, err := runCeph(ctx, cfg, args...); err != nil {
		return errorResponse("CEPHFS_SNAPSHOT_RESTORE_FAILED", err), nil
	}
	return success(map[string]string{"share_id": id, "snapshot": snapshot, "status": "restored"}), nil
}

func handleCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name   string  `json:"name"`
		SizeGb float64 `json:"sizeGb"`
		Size   float64 `json:"size"`
		Group  string  `json:"group"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.Name); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if input.SizeGb <= 0 {
		input.SizeGb = input.Size
	}
	cfg, err := loadConfig(input.Group)
	if err != nil {
		return errorResponse("CEPHFS_CONFIG_ERROR", err), nil
	}
	args := []string{"fs", "subvolume", "create", cfg.name, input.Name}
	if input.SizeGb > 0 {
		args = append(args, "--size", strconv.FormatInt(int64(input.SizeGb*1024*1024*1024), 10))
	}
	if cfg.group != "" {
		args = append(args, "--group", cfg.group)
	}
	if _, err := runCeph(ctx, cfg, args...); err != nil {
		return errorResponse("CEPHFS_CREATE_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": cfg.name + "/" + input.Name, "name": input.Name, "filesystem": cfg.name, "group": cfg.group, "sizeGb": input.SizeGb, "status": "available"}), nil
}

func handleDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, name, err := parseID(input.ID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	args := []string{"fs", "subvolume", "rm", cfg.name, name}
	if cfg.group != "" {
		args = append(args, "--group", cfg.group)
	}
	args = append(args, "--force")
	if _, err := runCeph(ctx, cfg, args...); err != nil {
		return errorResponse("CEPHFS_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"id": input.ID, "status": "deleted"}), nil
}

func handleResize(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID     string  `json:"id"`
		SizeGb float64 `json:"sizeGb"`
		Size   float64 `json:"size"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if input.SizeGb <= 0 {
		input.SizeGb = input.Size
	}
	if input.SizeGb <= 0 {
		return errorResponse("INVALID_INPUT", fmt.Errorf("sizeGb must be positive")), nil
	}
	cfg, name, err := parseID(input.ID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	args := []string{"fs", "subvolume", "resize", cfg.name, name, strconv.FormatInt(int64(input.SizeGb*1024*1024*1024), 10)}
	if cfg.group != "" {
		args = append(args, "--group", cfg.group)
	}
	if _, err := runCeph(ctx, cfg, args...); err != nil {
		return errorResponse("CEPHFS_RESIZE_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": input.ID, "sizeGb": input.SizeGb, "status": "available"}), nil
}

func handleMount(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID           string `json:"id"`
		ShareID      string `json:"share_id"`
		FilesystemID string `json:"filesystem_id"`
		VolumeID     string `json:"volume_id"`
		MountPoint   string `json:"mount_point"`
		Options      string `json:"mount_options"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	id := firstNonEmpty(input.ID, input.ShareID, input.FilesystemID, input.VolumeID)
	cfg, name, err := parseID(id)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if input.MountPoint == "" {
		return errorResponse("INVALID_INPUT", fmt.Errorf("mount_point is required")), nil
	}
	if err := os.MkdirAll(input.MountPoint, 0750); err != nil {
		return errorResponse("CEPHFS_MOUNT_FAILED", err), nil
	}
	path, err := subvolumePath(ctx, cfg, name)
	if err != nil {
		return errorResponse("CEPHFS_PATH_FAILED", err), nil
	}
	options := input.Options
	if options == "" {
		options = mountOptions(cfg)
	}
	if err := mountCephFS(ctx, cfg, path, input.MountPoint, options); err != nil {
		return errorResponse("CEPHFS_MOUNT_FAILED", err), nil
	}
	return success(map[string]string{"id": id, "mount_point": input.MountPoint, "path": path, "status": "mounted"}), nil
}

func handleUnmount(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
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
		return errorResponse("CEPHFS_UNMOUNT_FAILED", err), nil
	}
	return success(map[string]string{"mount_point": input.MountPoint, "status": "unmounted"}), nil
}

func handlePrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	cfg, name, err := parseID(req.SourceResourceID)
	if err != nil {
		return relationshipError("INVALID_INPUT", err), nil
	}
	path, err := subvolumePath(ctx, cfg, name)
	if err != nil {
		return relationshipError("CEPHFS_PATH_FAILED", err), nil
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"protocol": "cephfs", "filesystem": cfg.name, "path": path, "monitors": cfg.monitors, "instance_id": req.TargetResourceID})}, nil
}

func handleRelease(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	var input struct {
		MountPoint string `json:"mount_point"`
	}
	if len(req.Payload) > 0 {
		_ = decode(req.Payload, &input)
	}
	if input.MountPoint != "" {
		if _, err := runCommand(ctx, "umount", input.MountPoint); err != nil {
			return relationshipError("CEPHFS_UNMOUNT_FAILED", err), nil
		}
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"status": "released"})}, nil
}

func loadConfig(group string) (cephFSConfig, error) {
	name := os.Getenv("ORION_CEPHFS_NAME")
	monitors := os.Getenv("ORION_CEPHFS_MONITORS")
	if name == "" || monitors == "" {
		return cephFSConfig{}, fmt.Errorf("ORION_CEPHFS_NAME and ORION_CEPHFS_MONITORS are required")
	}
	mountRoot := envOr("ORION_CEPHFS_MOUNT_ROOT", "/srv/orion/cephfs")
	if !filepath.IsAbs(mountRoot) {
		return cephFSConfig{}, fmt.Errorf("ORION_CEPHFS_MOUNT_ROOT must be absolute")
	}
	return cephFSConfig{name: name, monitors: monitors, user: envOr("ORION_CEPHFS_USER", "admin"), conf: os.Getenv("ORION_CEPHFS_CONF"), keyring: os.Getenv("ORION_CEPHFS_KEYRING"), group: firstNonEmpty(group, os.Getenv("ORION_CEPHFS_GROUP")), mountRoot: filepath.Clean(mountRoot)}, nil
}

func parseID(id string) (cephFSConfig, string, error) {
	parts := strings.Split(strings.TrimPrefix(id, "cephfs/"), "/")
	if len(parts) == 1 {
		if err := validateName(parts[0]); err != nil {
			return cephFSConfig{}, "", err
		}
		cfg, err := loadConfig("")
		return cfg, parts[0], err
	}
	if len(parts) != 2 {
		return cephFSConfig{}, "", fmt.Errorf("CephFS id must be filesystem/name")
	}
	if err := validateName(parts[0]); err != nil {
		return cephFSConfig{}, "", err
	}
	if err := validateName(parts[1]); err != nil {
		return cephFSConfig{}, "", err
	}
	cfg, err := loadConfig("")
	if err != nil {
		return cephFSConfig{}, "", err
	}
	if cfg.name != parts[0] {
		return cephFSConfig{}, "", fmt.Errorf("filesystem %q does not match configured filesystem %q", parts[0], cfg.name)
	}
	return cfg, parts[1], nil
}

func subvolumePath(ctx context.Context, cfg cephFSConfig, name string) (string, error) {
	args := []string{"fs", "subvolume", "getpath", cfg.name, name}
	if cfg.group != "" {
		args = append(args, "--group", cfg.group)
	}
	path, err := runCeph(ctx, cfg, args...)
	return strings.TrimSpace(path), err
}

func mountCephFS(ctx context.Context, cfg cephFSConfig, path, mountPoint, options string) error {
	args := []string{"-t", "ceph", cfg.monitors + ":" + path, mountPoint}
	if options != "" {
		args = append([]string{"-t", "ceph", "-o", options}, cfg.monitors+":"+path, mountPoint)
	}
	_, err := runCommand(ctx, "mount", args...)
	return err
}

func mountOptions(cfg cephFSConfig) string {
	options := "name=" + cfg.user
	if cfg.conf != "" {
		options += ",conf=" + cfg.conf
	}
	if cfg.keyring != "" {
		options += ",secretfile=" + cfg.keyring
	}
	return options
}

func runCeph(ctx context.Context, cfg cephFSConfig, args ...string) (string, error) {
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
	return runCommand(ctx, "ceph", append(global, args...)...)
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
func marshal(value interface{}) []byte            { data, _ := json.Marshal(value); return data }
func decode(data []byte, value interface{}) error { return json.Unmarshal(data, value) }
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
