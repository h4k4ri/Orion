package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/horizon/orion/sdk/go/plugin"
)

type iscsiConfig struct {
	portal       string
	targetPrefix string
	root         string
}

func main() {
	p := plugin.New(plugin.Config{ID: "iscsi-plugin", Name: "iSCSI", Version: "1.0.0", Vendor: "Linux LIO"})
	p.Resource("orion.io/storage.volume", "v1").
		Handle("create", handleCreate).
		Handle("delete", handleDelete).
		Handle("attach", handleAttach).
		Handle("detach", handleDetach).
		AddCapability("multi_attach", true).
		Register()
	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", handlePrepare).
		Handle("release", handleRelease).
		Register()

	endpoint := envOr("ORION_PLUGIN_ENDPOINT", ":50057")
	log.Printf("iSCSI plugin starting on %s", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func handleCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Name        string  `json:"name"`
		SizeGb      float64 `json:"sizeGb"`
		Size        float64 `json:"size"`
		BackingPath string  `json:"backing_path"`
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
	cfg, err := loadConfig()
	if err != nil {
		return errorResponse("ISCSI_CONFIG_ERROR", err), nil
	}
	backingPath := input.BackingPath
	createdFile := false
	if backingPath == "" {
		if input.SizeGb <= 0 {
			return errorResponse("INVALID_INPUT", fmt.Errorf("sizeGb is required when backing_path is absent")), nil
		}
		backingPath = filepath.Join(cfg.root, input.Name+".img")
		if err := os.MkdirAll(cfg.root, 0750); err != nil {
			return errorResponse("ISCSI_CREATE_FAILED", err), nil
		}
		if _, err := runCommand(ctx, "truncate", "-s", strconv.FormatInt(int64(input.SizeGb*1024*1024*1024), 10), backingPath); err != nil {
			return errorResponse("ISCSI_CREATE_FAILED", err), nil
		}
		createdFile = true
	}
	if !filepath.IsAbs(backingPath) {
		return errorResponse("INVALID_INPUT", fmt.Errorf("backing_path must be absolute")), nil
	}
	targetIQN := cfg.targetPrefix + ":" + input.Name
	if err := validateIQN(targetIQN); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	backstoreArgs := []string{"backstores/fileio", "create", input.Name, backingPath}
	if input.SizeGb > 0 {
		backstoreArgs = append(backstoreArgs, sizeArgument(input.SizeGb))
	}
	if _, err := runTargetCLI(ctx, backstoreArgs...); err != nil {
		if createdFile {
			_ = os.Remove(backingPath)
		}
		return errorResponse("ISCSI_BACKSTORE_FAILED", err), nil
	}
	cleanup := true
	defer func() {
		if cleanup {
			_, _ = runTargetCLI(context.Background(), "iscsi/"+targetIQN, "delete")
			_, _ = runTargetCLI(context.Background(), "backstores/fileio", input.Name, "delete")
			if createdFile {
				_ = os.Remove(backingPath)
			}
		}
	}()
	if _, err := runTargetCLI(ctx, "iscsi", "create", targetIQN); err != nil {
		return errorResponse("ISCSI_TARGET_FAILED", err), nil
	}
	if _, err := runTargetCLI(ctx, "iscsi/"+targetIQN+"/tpg1/luns", "create", "/backstores/fileio/"+input.Name); err != nil {
		return errorResponse("ISCSI_LUN_FAILED", err), nil
	}
	host, port, err := splitPortal(cfg.portal)
	if err != nil {
		return errorResponse("ISCSI_CONFIG_ERROR", err), nil
	}
	if _, err := runTargetCLI(ctx, "iscsi/"+targetIQN+"/tpg1/portals", "create", host, port); err != nil {
		return errorResponse("ISCSI_PORTAL_FAILED", err), nil
	}
	cleanup = false
	return success(map[string]interface{}{"id": input.Name, "name": input.Name, "target_iqn": targetIQN, "portal": cfg.portal, "lun": 0, "backing_path": backingPath, "status": "available"}), nil
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
		return errorResponse("ISCSI_CONFIG_ERROR", err), nil
	}
	targetIQN := cfg.targetPrefix + ":" + input.ID
	if _, err := runTargetCLI(ctx, "iscsi/"+targetIQN, "delete"); err != nil {
		return errorResponse("ISCSI_TARGET_DELETE_FAILED", err), nil
	}
	if _, err := runTargetCLI(ctx, "backstores/fileio", input.ID, "delete"); err != nil {
		return errorResponse("ISCSI_BACKSTORE_DELETE_FAILED", err), nil
	}
	backingPath := filepath.Join(cfg.root, input.ID+".img")
	if err := os.Remove(backingPath); err != nil && !os.IsNotExist(err) {
		return errorResponse("ISCSI_FILE_DELETE_FAILED", err), nil
	}
	return success(map[string]string{"id": input.ID, "status": "deleted"}), nil
}

func handleAttach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		VolumeID  string `json:"volume_id"`
		TargetIQN string `json:"target_iqn"`
		Portal    string `json:"portal"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return errorResponse("ISCSI_CONFIG_ERROR", err), nil
	}
	if input.TargetIQN == "" {
		if err := validateName(input.VolumeID); err != nil {
			return errorResponse("INVALID_INPUT", err), nil
		}
		input.TargetIQN = cfg.targetPrefix + ":" + input.VolumeID
	}
	portal := firstNonEmpty(input.Portal, cfg.portal)
	if _, err := runCommand(ctx, "iscsiadm", "-m", "node", "-T", input.TargetIQN, "-p", portal, "--login"); err != nil {
		return errorResponse("ISCSI_LOGIN_FAILED", err), nil
	}
	return success(map[string]string{"target_iqn": input.TargetIQN, "portal": portal, "status": "attached"}), nil
}

func handleDetach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		TargetIQN string `json:"target_iqn"`
		Portal    string `json:"portal"`
	}
	if err := decode(req.Payload, &input); err != nil || input.TargetIQN == "" {
		if err == nil {
			err = fmt.Errorf("target_iqn is required")
		}
		return errorResponse("INVALID_INPUT", err), nil
	}
	args := []string{"-m", "node", "-T", input.TargetIQN}
	if input.Portal != "" {
		args = append(args, "-p", input.Portal)
	}
	args = append(args, "--logout")
	if _, err := runCommand(ctx, "iscsiadm", args...); err != nil {
		return errorResponse("ISCSI_LOGOUT_FAILED", err), nil
	}
	return success(map[string]string{"target_iqn": input.TargetIQN, "status": "detached"}), nil
}

func handlePrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	if err := validateName(req.SourceResourceID); err != nil {
		return relationshipError("INVALID_INPUT", err), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return relationshipError("ISCSI_CONFIG_ERROR", err), nil
	}
	targetIQN := cfg.targetPrefix + ":" + req.SourceResourceID
	if _, err := runTargetCLI(ctx, "iscsi/"+targetIQN+"/tpg1", "ls"); err != nil {
		return relationshipError("ISCSI_TARGET_NOT_FOUND", err), nil
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]interface{}{"protocol": "iscsi", "target_iqn": targetIQN, "portal": cfg.portal, "lun": 0, "instance_id": req.TargetResourceID})}, nil
}

func handleRelease(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	var input struct{ TargetIQN, Portal string }
	if len(req.Payload) > 0 {
		_ = decode(req.Payload, &input)
	}
	if input.TargetIQN != "" {
		args := []string{"-m", "node", "-T", input.TargetIQN}
		if input.Portal != "" {
			args = append(args, "-p", input.Portal)
		}
		args = append(args, "--logout")
		if _, err := runCommand(ctx, "iscsiadm", args...); err != nil {
			return relationshipError("ISCSI_LOGOUT_FAILED", err), nil
		}
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"status": "released"})}, nil
}

func loadConfig() (iscsiConfig, error) {
	portal := envOr("ORION_ISCSI_PORTAL", "0.0.0.0:3260")
	if _, _, err := splitPortal(portal); err != nil {
		return iscsiConfig{}, err
	}
	root := envOr("ORION_ISCSI_BACKING_ROOT", "/var/lib/orion/iscsi")
	if !filepath.IsAbs(root) {
		return iscsiConfig{}, fmt.Errorf("ORION_ISCSI_BACKING_ROOT must be absolute")
	}
	return iscsiConfig{portal: portal, targetPrefix: envOr("ORION_ISCSI_TARGET_PREFIX", "iqn.2026-01.io.orion"), root: filepath.Clean(root)}, nil
}

func splitPortal(portal string) (string, string, error) {
	host, port, err := net.SplitHostPort(portal)
	if err == nil {
		return host, port, nil
	}
	index := strings.LastIndex(portal, ":")
	if index < 1 || index == len(portal)-1 {
		return "", "", fmt.Errorf("portal must be host:port")
	}
	return portal[:index], portal[index+1:], nil
}

func runTargetCLI(ctx context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("targetcli path is required")
	}
	path := args[0]
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return runCommand(ctx, "targetcli", append([]string{path}, args[1:]...)...)
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func sizeArgument(sizeGb float64) string {
	if sizeGb <= 0 {
		return "0"
	}
	return strconv.FormatInt(int64(sizeGb*1024*1024*1024), 10)
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

func validateIQN(value string) error {
	if value == "" || strings.ContainsAny(value, " \t\r\n/") {
		return fmt.Errorf("invalid iSCSI target IQN")
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
