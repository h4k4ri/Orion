package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/horizon/orion/sdk/go/plugin"
)

type nfsConfig struct {
	server  string
	root    string
	clients string
	options string
}

func main() {
	p := plugin.New(plugin.Config{ID: "nfs-plugin", Name: "NFS", Version: "2.0.0", Vendor: "Linux"})
	p.Resource("orion.io/storage.volume", "v1").
		Handle("create", handleVolumeCreate).
		Handle("delete", handleVolumeDelete).
		Handle("attach", handleVolumeAttach).
		AddCapability("shared", true).
		AddCapability("concurrent_access", true).
		Register()
	p.Resource("orion.io/storage.share", "v1").
		Handle("create", handleVolumeCreate).
		Handle("delete", handleVolumeDelete).
		Handle("attach", handleVolumeAttach).
		Handle("detach", handleVolumeDetach).
		AddCapability("concurrent_access", true).
		Register()
	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", handleAttachmentPrepare).
		Handle("release", handleAttachmentRelease).
		Register()

	endpoint := envOr("ORION_PLUGIN_ENDPOINT", ":50054")
	log.Printf("NFS plugin starting on %s", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func handleVolumeCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name       string `json:"name"`
		ExportPath string `json:"export_path"`
		Path       string `json:"path"`
		Size       int64  `json:"size"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadNFSConfig()
	if err != nil {
		return errorResponse("NFS_CONFIG_ERROR", err), nil
	}
	path, name, err := cfg.volumePath(input.Name, firstNonEmpty(input.ExportPath, input.Path))
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := os.MkdirAll(path, 0750); err != nil {
		return errorResponse("NFS_CREATE_FAILED", err), nil
	}
	if err := exportPath(ctx, cfg, path); err != nil {
		return errorResponse("NFS_EXPORT_FAILED", err), nil
	}
	return success(map[string]interface{}{
		"id": name, "name": name, "export_path": path, "server": cfg.server,
		"size": input.Size, "status": "exported", "mount_options": cfg.options,
	}), nil
}

func handleVolumeDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadNFSConfig()
	if err != nil {
		return errorResponse("NFS_CONFIG_ERROR", err), nil
	}
	path, _, err := cfg.volumePath(input.ID, input.ID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := unexportPath(ctx, cfg, path); err != nil {
		return errorResponse("NFS_UNEXPORT_FAILED", err), nil
	}
	if err := os.RemoveAll(path); err != nil {
		return errorResponse("NFS_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"id": input.ID, "status": "deleted"}), nil
}

func handleVolumeAttach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		VolumeID   string `json:"volume_id"`
		ShareID    string `json:"share_id"`
		ID         string `json:"id"`
		MountPoint string `json:"mount_point"`
		Options    string `json:"mount_options"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadNFSConfig()
	if err != nil {
		return errorResponse("NFS_CONFIG_ERROR", err), nil
	}
	volumeID := firstNonEmpty(input.VolumeID, input.ShareID, input.ID)
	path, _, err := cfg.volumePath(volumeID, volumeID)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if input.MountPoint == "" {
		return errorResponse("INVALID_INPUT", fmt.Errorf("mount_point is required for a real NFS attach")), nil
	}
	if err := os.MkdirAll(input.MountPoint, 0750); err != nil {
		return errorResponse("NFS_MOUNT_FAILED", err), nil
	}
	options := input.Options
	if options == "" {
		options = cfg.options
	}
	if err := runMount(ctx, cfg.server+":"+path, input.MountPoint, options); err != nil {
		return errorResponse("NFS_MOUNT_FAILED", err), nil
	}
	return success(map[string]string{"volume_id": volumeID, "mount_point": input.MountPoint, "status": "attached"}), nil
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
		return errorResponse("NFS_UNMOUNT_FAILED", err), nil
	}
	return success(map[string]string{"mount_point": input.MountPoint, "status": "detached"}), nil
}

func handleAttachmentPrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	cfg, err := loadNFSConfig()
	if err != nil {
		return relationshipError("NFS_CONFIG_ERROR", err), nil
	}
	path, _, err := cfg.volumePath(req.SourceResourceID, req.SourceResourceID)
	if err != nil {
		return relationshipError("INVALID_INPUT", err), nil
	}
	if _, err := os.Stat(path); err != nil {
		return relationshipError("NFS_NOT_FOUND", err), nil
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{
		"protocol": "nfs4", "server": cfg.server, "export_path": path,
		"instance_id": req.TargetResourceID, "mount_options": cfg.options,
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
			return relationshipError("NFS_UNMOUNT_FAILED", err), nil
		}
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"status": "released"})}, nil
}

func loadNFSConfig() (nfsConfig, error) {
	server := os.Getenv("ORION_NFS_SERVER")
	if server == "" {
		return nfsConfig{}, fmt.Errorf("ORION_NFS_SERVER is required")
	}
	root := envOr("ORION_NFS_EXPORT_ROOT", "/srv/orion/nfs")
	if !filepath.IsAbs(root) {
		return nfsConfig{}, fmt.Errorf("ORION_NFS_EXPORT_ROOT must be absolute")
	}
	return nfsConfig{server: server, root: filepath.Clean(root), clients: envOr("ORION_NFS_CLIENTS", "*"), options: envOr("ORION_NFS_EXPORT_OPTIONS", "rw,sync,no_subtree_check")}, nil
}

func (c nfsConfig) volumePath(name, explicit string) (string, string, error) {
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			return "", "", fmt.Errorf("export_path must be absolute")
		}
		path := filepath.Clean(explicit)
		if !withinRoot(c.root, path) {
			return "", "", fmt.Errorf("export_path must be below ORION_NFS_EXPORT_ROOT")
		}
		return path, filepath.Base(path), nil
	}
	if err := validateName(name); err != nil {
		return "", "", err
	}
	return filepath.Join(c.root, name), name, nil
}

func exportPath(ctx context.Context, cfg nfsConfig, path string) error {
	_, err := runCommand(ctx, "exportfs", "-i", "-o", cfg.options, cfg.clients+":"+path)
	return err
}

func unexportPath(ctx context.Context, cfg nfsConfig, path string) error {
	_, err := runCommand(ctx, "exportfs", "-u", cfg.clients+":"+path)
	return err
}

func runMount(ctx context.Context, source, target, options string) error {
	args := []string{"-t", "nfs4"}
	if options != "" {
		args = append(args, "-o", options)
	}
	_, err := runCommand(ctx, "mount", append(args, source, target)...)
	return err
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
