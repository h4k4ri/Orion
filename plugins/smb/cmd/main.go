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

type smbConfig struct {
	server       string
	shareRoot    string
	configFile   string
	validUsers   string
	mountOptions string
}

func main() {
	p := plugin.New(plugin.Config{ID: "smb-plugin", Name: "SMB", Version: "1.0.0", Vendor: "Samba"})
	p.Resource("orion.io/storage.share", "v1").
		Handle("create", handleCreate).
		Handle("delete", handleDelete).
		Handle("attach", handleAttach).
		Handle("detach", handleDetach).
		AddCapability("concurrent_access", true).
		AddCapability("quotas", false).
		Register()
	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", handlePrepare).
		Handle("release", handleRelease).
		Register()

	endpoint := envOr("ORION_PLUGIN_ENDPOINT", ":50058")
	log.Printf("SMB plugin starting on %s", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func handleCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		ReadOnly bool   `json:"read_only"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.Name); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return errorResponse("SMB_CONFIG_ERROR", err), nil
	}
	path, err := sharePath(cfg, input.Name, input.Path)
	if err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := os.MkdirAll(path, 0750); err != nil {
		return errorResponse("SMB_CREATE_FAILED", err), nil
	}
	block := renderShare(input.Name, path, input.ReadOnly, cfg.validUsers)
	if err := applyShare(ctx, cfg, input.Name, block, false); err != nil {
		return errorResponse("SMB_CONFIGURE_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": input.Name, "name": input.Name, "path": path, "server": cfg.server, "read_only": input.ReadOnly, "status": "available"}), nil
}

func handleDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if err := validateName(input.ID); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return errorResponse("SMB_CONFIG_ERROR", err), nil
	}
	if err := applyShare(ctx, cfg, input.ID, "", true); err != nil {
		return errorResponse("SMB_DELETE_FAILED", err), nil
	}
	path, _ := sharePath(cfg, input.ID, "")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = err
	}
	return success(map[string]string{"id": input.ID, "status": "deleted"}), nil
}

func handleAttach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ShareID    string `json:"share_id"`
		ID         string `json:"id"`
		MountPoint string `json:"mount_point"`
		Options    string `json:"mount_options"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	shareID := firstNonEmpty(input.ShareID, input.ID)
	if err := validateName(shareID); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	if input.MountPoint == "" {
		return errorResponse("INVALID_INPUT", fmt.Errorf("mount_point is required")), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return errorResponse("SMB_CONFIG_ERROR", err), nil
	}
	if err := os.MkdirAll(input.MountPoint, 0750); err != nil {
		return errorResponse("SMB_MOUNT_FAILED", err), nil
	}
	options := firstNonEmpty(input.Options, cfg.mountOptions)
	args := []string{"-t", "cifs", "//" + cfg.server + "/" + shareID, input.MountPoint}
	if options != "" {
		args = append(args[:len(args)-2], "-o", options, args[len(args)-2], args[len(args)-1])
	}
	if _, err := runCommand(ctx, "mount", args...); err != nil {
		return errorResponse("SMB_MOUNT_FAILED", err), nil
	}
	return success(map[string]string{"id": shareID, "mount_point": input.MountPoint, "status": "attached"}), nil
}

func handleDetach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
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
		return errorResponse("SMB_UNMOUNT_FAILED", err), nil
	}
	return success(map[string]string{"mount_point": input.MountPoint, "status": "detached"}), nil
}

func handlePrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	if err := validateName(req.SourceResourceID); err != nil {
		return relationshipError("INVALID_INPUT", err), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return relationshipError("SMB_CONFIG_ERROR", err), nil
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"protocol": "smb3", "server": cfg.server, "share": req.SourceResourceID, "instance_id": req.TargetResourceID})}, nil
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
			return relationshipError("SMB_UNMOUNT_FAILED", err), nil
		}
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"status": "released"})}, nil
}

func loadConfig() (smbConfig, error) {
	server := os.Getenv("ORION_SMB_SERVER")
	if server == "" {
		return smbConfig{}, fmt.Errorf("ORION_SMB_SERVER is required")
	}
	root := envOr("ORION_SMB_SHARE_ROOT", "/srv/orion/smb")
	if !filepath.IsAbs(root) {
		return smbConfig{}, fmt.Errorf("ORION_SMB_SHARE_ROOT must be absolute")
	}
	configFile := envOr("ORION_SMB_CONFIG_FILE", "/etc/samba/smb.conf")
	if !filepath.IsAbs(configFile) {
		return smbConfig{}, fmt.Errorf("ORION_SMB_CONFIG_FILE must be absolute")
	}
	return smbConfig{server: server, shareRoot: filepath.Clean(root), configFile: filepath.Clean(configFile), validUsers: os.Getenv("ORION_SMB_VALID_USERS"), mountOptions: os.Getenv("ORION_SMB_MOUNT_OPTIONS")}, nil
}

func sharePath(cfg smbConfig, name, explicit string) (string, error) {
	if explicit == "" {
		return filepath.Join(cfg.shareRoot, name), nil
	}
	if !filepath.IsAbs(explicit) {
		return "", fmt.Errorf("path must be absolute")
	}
	path := filepath.Clean(explicit)
	if !withinRoot(cfg.shareRoot, path) {
		return "", fmt.Errorf("path must be below ORION_SMB_SHARE_ROOT")
	}
	return path, nil
}

func renderShare(name, path string, readOnly bool, validUsers string) string {
	mode := "no"
	if readOnly {
		mode = "yes"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "# BEGIN ORION SHARE %s\n[%s]\npath = %s\nbrowseable = yes\nread only = %s\nguest ok = no\n", name, name, path, mode)
	if validUsers != "" {
		fmt.Fprintf(&builder, "valid users = %s\n", validUsers)
	}
	builder.WriteString("# END ORION SHARE " + name + "\n")
	return builder.String()
}

func applyShare(ctx context.Context, cfg smbConfig, name, block string, remove bool) error {
	old, err := os.ReadFile(cfg.configFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated := removeManagedBlock(string(old), name)
	if !remove {
		updated = strings.TrimRight(updated, "\n") + "\n\n" + block
	}
	if err := os.MkdirAll(filepath.Dir(cfg.configFile), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(cfg.configFile), ".orion-smb-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(updated); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if _, err := runCommand(ctx, "testparm", "-s", tmpName); err != nil {
		return fmt.Errorf("validate Samba configuration: %w", err)
	}
	if err := os.Rename(tmpName, cfg.configFile); err != nil {
		return err
	}
	if _, err := runCommand(ctx, "smbcontrol", "all", "reload-config"); err != nil {
		return err
	}
	return nil
}

func removeManagedBlock(config, name string) string {
	startMarker := "# BEGIN ORION SHARE " + name
	endMarker := "# END ORION SHARE " + name
	for {
		start := strings.Index(config, startMarker)
		if start < 0 {
			return config
		}
		end := strings.Index(config[start:], endMarker)
		if end < 0 {
			return config[:start]
		}
		end += start + len(endMarker)
		if end < len(config) && config[end] == '\n' {
			end++
		}
		config = config[:start] + config[end:]
	}
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
