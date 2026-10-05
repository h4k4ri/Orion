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

type nvmeConfig struct {
	traddr     string
	trsvcid    string
	nqnPrefix  string
	root       string
	configFile string
}

type nvmetFile struct {
	Ports      []nvmetPort      `json:"ports"`
	Subsystems []nvmetSubsystem `json:"subsystems"`
}

type nvmetPort struct {
	PortID     int       `json:"portid"`
	Addr       nvmetAddr `json:"addr"`
	Subsystems []string  `json:"subsystems"`
}

type nvmetAddr struct {
	Trtype  string `json:"trtype"`
	Adrfam  string `json:"adrfam"`
	Traddr  string `json:"traddr"`
	Trsvcid string `json:"trsvcid"`
}

type nvmetSubsystem struct {
	NQN          string           `json:"nqn"`
	AllowAnyHost int              `json:"attr_allow_any_host"`
	Namespaces   []nvmetNamespace `json:"namespaces"`
}

type nvmetNamespace struct {
	Device nvmetDevice `json:"device"`
	Enable int         `json:"enable"`
	NSID   int         `json:"nsid"`
}

type nvmetDevice struct {
	Path string `json:"path"`
}

func main() {
	p := plugin.New(plugin.Config{ID: "nvmeof-plugin", Name: "NVMe-oF", Version: "1.0.0", Vendor: "Linux NVMe target"})
	p.Resource("orion.io/storage.volume", "v1").
		Handle("create", handleCreate).
		Handle("delete", handleDelete).
		Handle("resize", handleResize).
		Handle("attach", handleAttach).
		Handle("detach", handleDetach).
		AddCapability("multi_attach", true).
		Register()
	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", handlePrepare).
		Handle("release", handleRelease).
		Register()

	endpoint := envOr("ORION_PLUGIN_ENDPOINT", ":50059")
	log.Printf("NVMe-oF plugin starting on %s", endpoint)
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
		return errorResponse("NVMEOF_CONFIG_ERROR", err), nil
	}
	backingPath := input.BackingPath
	if backingPath == "" {
		if input.SizeGb <= 0 {
			return errorResponse("INVALID_INPUT", fmt.Errorf("sizeGb is required when backing_path is absent")), nil
		}
		backingPath = filepath.Join(cfg.root, input.Name+".img")
		if err := os.MkdirAll(cfg.root, 0750); err != nil {
			return errorResponse("NVMEOF_CREATE_FAILED", err), nil
		}
		if _, err := runCommand(ctx, "truncate", "-s", bytesForGB(input.SizeGb), backingPath); err != nil {
			return errorResponse("NVMEOF_CREATE_FAILED", err), nil
		}
	}
	if !filepath.IsAbs(backingPath) {
		return errorResponse("INVALID_INPUT", fmt.Errorf("backing_path must be absolute")), nil
	}
	nqn := cfg.nqnPrefix + ":" + input.Name
	file, err := readConfig(cfg.configFile)
	if err != nil {
		return errorResponse("NVMEOF_CONFIG_ERROR", err), nil
	}
	if containsSubsystem(file, nqn) {
		return errorResponse("NVMEOF_CREATE_FAILED", fmt.Errorf("subsystem %s already exists", nqn)), nil
	}
	file.Subsystems = append(file.Subsystems, nvmetSubsystem{NQN: nqn, AllowAnyHost: 1, Namespaces: []nvmetNamespace{{Device: nvmetDevice{Path: backingPath}, Enable: 1, NSID: 1}}})
	port := ensurePort(&file, cfg)
	if !containsString(port.Subsystems, nqn) {
		port.Subsystems = append(port.Subsystems, nqn)
	}
	if err := applyConfig(ctx, cfg.configFile, file); err != nil {
		return errorResponse("NVMEOF_APPLY_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": input.Name, "nqn": nqn, "traddr": cfg.traddr, "trsvcid": cfg.trsvcid, "backing_path": backingPath, "status": "available"}), nil
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
		return errorResponse("NVMEOF_CONFIG_ERROR", err), nil
	}
	file, err := readConfig(cfg.configFile)
	if err != nil {
		return errorResponse("NVMEOF_CONFIG_ERROR", err), nil
	}
	nqn := cfg.nqnPrefix + ":" + input.ID
	var backingPath string
	kept := file.Subsystems[:0]
	for _, subsystem := range file.Subsystems {
		if subsystem.NQN == nqn {
			if len(subsystem.Namespaces) > 0 {
				backingPath = subsystem.Namespaces[0].Device.Path
			}
			continue
		}
		kept = append(kept, subsystem)
	}
	file.Subsystems = kept
	for i := range file.Ports {
		file.Ports[i].Subsystems = removeString(file.Ports[i].Subsystems, nqn)
	}
	if err := applyConfig(ctx, cfg.configFile, file); err != nil {
		return errorResponse("NVMEOF_DELETE_FAILED", err), nil
	}
	if backingPath == filepath.Join(cfg.root, input.ID+".img") {
		_ = os.Remove(backingPath)
	}
	return success(map[string]string{"id": input.ID, "status": "deleted"}), nil
}

func handleResize(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID     string  `json:"id"`
		SizeGb float64 `json:"sizeGb"`
	}
	if err := decode(req.Payload, &input); err != nil || input.SizeGb <= 0 {
		if err == nil {
			err = fmt.Errorf("sizeGb must be positive")
		}
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return errorResponse("NVMEOF_CONFIG_ERROR", err), nil
	}
	file, err := readConfig(cfg.configFile)
	if err != nil {
		return errorResponse("NVMEOF_CONFIG_ERROR", err), nil
	}
	nqn := cfg.nqnPrefix + ":" + input.ID
	var backingPath string
	for _, subsystem := range file.Subsystems {
		if subsystem.NQN == nqn && len(subsystem.Namespaces) > 0 {
			backingPath = subsystem.Namespaces[0].Device.Path
		}
	}
	if backingPath == "" {
		return errorResponse("NVMEOF_NOT_FOUND", fmt.Errorf("subsystem %s not found", nqn)), nil
	}
	if strings.HasPrefix(backingPath, "/dev/") {
		return errorResponse("NVMEOF_RESIZE_FAILED", fmt.Errorf("resize of block-device namespaces must be performed by the underlying storage provider")), nil
	}
	if _, err := runCommand(ctx, "truncate", "-s", bytesForGB(input.SizeGb), backingPath); err != nil {
		return errorResponse("NVMEOF_RESIZE_FAILED", err), nil
	}
	return success(map[string]interface{}{"id": input.ID, "sizeGb": input.SizeGb, "status": "available"}), nil
}

func handleAttach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		VolumeID string `json:"volume_id"`
		NQN      string `json:"nqn"`
		Address  string `json:"traddr"`
		Service  string `json:"trsvcid"`
	}
	if err := decode(req.Payload, &input); err != nil {
		return errorResponse("INVALID_INPUT", err), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return errorResponse("NVMEOF_CONFIG_ERROR", err), nil
	}
	nqn := firstNonEmpty(input.NQN, cfg.nqnPrefix+":"+input.VolumeID)
	address, service := firstNonEmpty(input.Address, cfg.traddr), firstNonEmpty(input.Service, cfg.trsvcid)
	if _, err := runCommand(ctx, "nvme", "connect", "-t", "tcp", "-n", nqn, "-a", address, "-s", service); err != nil {
		return errorResponse("NVMEOF_CONNECT_FAILED", err), nil
	}
	return success(map[string]string{"nqn": nqn, "traddr": address, "trsvcid": service, "status": "attached"}), nil
}

func handleDetach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		NQN string `json:"nqn"`
	}
	if err := decode(req.Payload, &input); err != nil || input.NQN == "" {
		if err == nil {
			err = fmt.Errorf("nqn is required")
		}
		return errorResponse("INVALID_INPUT", err), nil
	}
	if _, err := runCommand(ctx, "nvme", "disconnect", "-n", input.NQN); err != nil {
		return errorResponse("NVMEOF_DISCONNECT_FAILED", err), nil
	}
	return success(map[string]string{"nqn": input.NQN, "status": "detached"}), nil
}

func handlePrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	if err := validateName(req.SourceResourceID); err != nil {
		return relationshipError("INVALID_INPUT", err), nil
	}
	cfg, err := loadConfig()
	if err != nil {
		return relationshipError("NVMEOF_CONFIG_ERROR", err), nil
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"protocol": "nvmeof", "transport": "tcp", "nqn": cfg.nqnPrefix + ":" + req.SourceResourceID, "traddr": cfg.traddr, "trsvcid": cfg.trsvcid, "instance_id": req.TargetResourceID})}, nil
}

func handleRelease(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	var input struct {
		NQN string `json:"nqn"`
	}
	if len(req.Payload) > 0 {
		_ = decode(req.Payload, &input)
	}
	if input.NQN != "" {
		if _, err := runCommand(ctx, "nvme", "disconnect", "-n", input.NQN); err != nil {
			return relationshipError("NVMEOF_DISCONNECT_FAILED", err), nil
		}
	}
	return &plugin.RelationshipResponse{Success: true, Result: marshal(map[string]string{"status": "released"})}, nil
}

func loadConfig() (nvmeConfig, error) {
	traddr := os.Getenv("ORION_NVMEOF_TRADDR")
	if traddr == "" {
		return nvmeConfig{}, fmt.Errorf("ORION_NVMEOF_TRADDR is required")
	}
	root := envOr("ORION_NVMEOF_BACKING_ROOT", "/var/lib/orion/nvmeof")
	configFile := envOr("ORION_NVMEOF_CONFIG", "/etc/nvmet/config.json")
	if !filepath.IsAbs(root) || !filepath.IsAbs(configFile) {
		return nvmeConfig{}, fmt.Errorf("NVM-oF paths must be absolute")
	}
	return nvmeConfig{traddr: traddr, trsvcid: envOr("ORION_NVMEOF_TRSVCID", "4420"), nqnPrefix: envOr("ORION_NVMEOF_NQN_PREFIX", "nqn.2026-01.io.orion"), root: filepath.Clean(root), configFile: filepath.Clean(configFile)}, nil
}

func readConfig(path string) (nvmetFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nvmetFile{}, nil
	}
	if err != nil {
		return nvmetFile{}, err
	}
	var file nvmetFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nvmetFile{}, fmt.Errorf("decode nvmet config: %w", err)
	}
	return file, nil
}

func applyConfig(ctx context.Context, path string, file nvmetFile) error {
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".orion-nvmet-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if _, err := runCommand(ctx, "nvmetcli", "restore", tmpName); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func ensurePort(file *nvmetFile, cfg nvmeConfig) *nvmetPort {
	for i := range file.Ports {
		if file.Ports[i].Addr.Traddr == cfg.traddr && file.Ports[i].Addr.Trsvcid == cfg.trsvcid {
			return &file.Ports[i]
		}
	}
	portID := 1
	for _, port := range file.Ports {
		if port.PortID >= portID {
			portID = port.PortID + 1
		}
	}
	file.Ports = append(file.Ports, nvmetPort{PortID: portID, Addr: nvmetAddr{Trtype: "tcp", Adrfam: "ipv4", Traddr: cfg.traddr, Trsvcid: cfg.trsvcid}})
	return &file.Ports[len(file.Ports)-1]
}

func containsSubsystem(file nvmetFile, nqn string) bool {
	for _, item := range file.Subsystems {
		if item.NQN == nqn {
			return true
		}
	}
	return false
}
func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func removeString(values []string, unwanted string) []string {
	result := values[:0]
	for _, value := range values {
		if value != unwanted {
			result = append(result, value)
		}
	}
	return result
}
func bytesForGB(size float64) string { return strconv.FormatInt(int64(size*1024*1024*1024), 10) }
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
func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
