package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/horizon/orion/sdk/go/plugin"
)

var (
	resticPath    = "restic"
	repoEnvPrefix = "RESTIC_REPOSITORY"
	passEnvPrefix = "RESTIC_PASSWORD"
)

func main() {
	p := plugin.New(plugin.Config{
		ID:      "backup-plugin",
		Name:    "Backup",
		Version: "1.0.0",
		Vendor:  "Restic",
	})

	p.Resource("orion.io/backup.backup", "v1").
		Handle("init", handleBackupInit).
		Handle("create", handleBackupCreate).
		Handle("restore", handleBackupRestore).
		Handle("snapshots", handleBackupSnapshots).
		Handle("forget", handleBackupForget).
		Handle("prune", handleBackupPrune).
		Handle("check", handleBackupCheck).
		Handle("mount", handleBackupMount).
		Handle("cat", handleBackupCat).
		AddCapability("s3", true).
		AddCapability("nfs", true).
		AddCapability("local", true).
		AddCapability("encryption", true).
		AddCapability("compression", true).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50062"
	}
	log.Printf("Backup plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func handleBackupInit(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)

	if repository == "" {
		return errorResponse(fmt.Errorf("repository is required")), nil
	}
	if password == "" {
		password = os.Getenv("RESTIC_PASSWORD")
		if password == "" {
			password = "backup-password"
		}
	}

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	cmd := exec.CommandContext(ctx, resticPath, "init")
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "already initialized") {
			log.Printf("Backup: repository already initialized")
			return &plugin.ResourceResponse{
				Success: true,
				Result: marshal(map[string]interface{}{
					"repository": repository,
					"status":     "already_initialized",
				}),
			}, nil
		}
		return errorResponse(fmt.Errorf("restic init failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Backup: initialized repository %s", repository)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"repository": repository,
			"status":     "initialized",
		}),
	}, nil
}

func handleBackupCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	paths, _ := input["paths"].([]interface{})
	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)
	tags, _ := input["tags"].([]interface{})
	host, _ := input["host"].(string)

	if len(paths) == 0 {
		return errorResponse(fmt.Errorf("paths are required")), nil
	}
	if repository == "" {
		repository = os.Getenv(repoEnvPrefix)
	}
	if password == "" {
		password = os.Getenv(passEnvPrefix)
		if password == "" {
			password = "backup-password"
		}
	}
	if host == "" {
		host, _ = os.Hostname()
	}

	pathStrs := make([]string, len(paths))
	for i, p := range paths {
		pathStrs[i], _ = p.(string)
	}

	args := []string{"backup"}

	for _, tag := range tags {
		args = append(args, "--tag", tag.(string))
	}

	if host != "" {
		args = append(args, "--host", host)
	}

	args = append(args, "--json")
	args = append(args, pathStrs...)

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	cmd := exec.CommandContext(ctx, resticPath, args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("restic backup failed: %w, output: %s", err, string(output))), nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		result = map[string]interface{}{
			"output": string(output),
		}
	}

	snapshotID, _ := result["snapshot_id"].(string)
	log.Printf("Backup: created backup %s for paths %v", snapshotID, paths)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":          snapshotID,
			"snapshot_id": snapshotID,
			"paths":       paths,
			"host":        host,
			"tags":        tags,
			"repository":  repository,
			"status":      "created",
		}),
	}, nil
}

func handleBackupRestore(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	snapshotID, _ := input["snapshot_id"].(string)
	target, _ := input["target"].(string)
	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)

	if snapshotID == "" {
		return errorResponse(fmt.Errorf("snapshot_id is required")), nil
	}
	if target == "" {
		return errorResponse(fmt.Errorf("target is required")), nil
	}
	if repository == "" {
		repository = os.Getenv(repoEnvPrefix)
	}
	if password == "" {
		password = os.Getenv(passEnvPrefix)
		if password == "" {
			password = "backup-password"
		}
	}

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	args := []string{"restore", snapshotID, "--target", target, "--json"}

	cmd := exec.CommandContext(ctx, resticPath, args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("restic restore failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Backup: restored snapshot %s to %s", snapshotID, target)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":          snapshotID,
			"snapshot_id": snapshotID,
			"target":      target,
			"repository":  repository,
			"status":      "restored",
		}),
	}, nil
}

func handleBackupSnapshots(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)
	host, _ := input["host"].(string)
	path, _ := input["path"].(string)
	latest, _ := input["latest"].(bool)

	if repository == "" {
		repository = os.Getenv(repoEnvPrefix)
	}
	if password == "" {
		password = os.Getenv(passEnvPrefix)
		if password == "" {
			password = "backup-password"
		}
	}

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	args := []string{"snapshots", "--json"}
	if latest {
		args = append(args, "--latest", "1")
	}
	if host != "" {
		args = append(args, "--host", host)
	}
	if path != "" {
		args = append(args, "--path", path)
	}

	cmd := exec.CommandContext(ctx, resticPath, args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("restic snapshots failed: %w, output: %s", err, string(output))), nil
	}

	var snapshots []map[string]interface{}
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return errorResponse(fmt.Errorf("failed to parse snapshots: %w", err)), nil
	}

	log.Printf("Backup: listed %d snapshots", len(snapshots))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"repository": repository,
			"snapshots":  snapshots,
			"count":      len(snapshots),
		}),
	}, nil
}

func handleBackupForget(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	snapshotID, _ := input["snapshot_id"].(string)
	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)
	keepLast, _ := input["keep_last"].(float64)
	keepDaily, _ := input["keep_daily"].(float64)
	keepWeekly, _ := input["keep_weekly"].(float64)
	keepMonthly, _ := input["keep_monthly"].(float64)
	keepTags, _ := input["keep_tags"].([]interface{})

	if repository == "" {
		repository = os.Getenv(repoEnvPrefix)
	}
	if password == "" {
		password = os.Getenv(passEnvPrefix)
		if password == "" {
			password = "backup-password"
		}
	}

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	args := []string{"forget", "--json"}

	if snapshotID != "" {
		args = append(args, snapshotID)
	} else {
		if keepLast > 0 {
			args = append(args, "--keep-last", fmt.Sprintf("%d", int(keepLast)))
		}
		if keepDaily > 0 {
			args = append(args, "--keep-daily", fmt.Sprintf("%d", int(keepDaily)))
		}
		if keepWeekly > 0 {
			args = append(args, "--keep-weekly", fmt.Sprintf("%d", int(keepWeekly)))
		}
		if keepMonthly > 0 {
			args = append(args, "--keep-monthly", fmt.Sprintf("%d", int(keepMonthly)))
		}
		for _, tag := range keepTags {
			args = append(args, "--keep-tag", tag.(string))
		}
	}

	cmd := exec.CommandContext(ctx, resticPath, args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("restic forget failed: %w, output: %s", err, string(output))), nil
	}

	var removed []map[string]interface{}
	json.Unmarshal(output, &removed)

	log.Printf("Backup: forget completed, removed %d snapshots", len(removed))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"repository": repository,
			"removed":    removed,
			"count":      len(removed),
			"status":     "forgot",
		}),
	}, nil
}

func handleBackupPrune(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)

	if repository == "" {
		repository = os.Getenv(repoEnvPrefix)
	}
	if password == "" {
		password = os.Getenv(passEnvPrefix)
		if password == "" {
			password = "backup-password"
		}
	}

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	cmd := exec.CommandContext(ctx, resticPath, "prune", "--json")
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("restic prune failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Backup: pruned repository %s", repository)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"repository": repository,
			"status":     "pruned",
		}),
	}, nil
}

func handleBackupCheck(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)
	readData, _ := input["read_data"].(bool)

	if repository == "" {
		repository = os.Getenv(repoEnvPrefix)
	}
	if password == "" {
		password = os.Getenv(passEnvPrefix)
		if password == "" {
			password = "backup-password"
		}
	}

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	args := []string{"check", "--json"}
	if readData {
		args = append(args, "--read-data")
	}

	cmd := exec.CommandContext(ctx, resticPath, args...)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("restic check failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Backup: check completed for repository %s", repository)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"repository": repository,
			"status":     "checked",
			"output":     string(output),
		}),
	}, nil
}

func handleBackupMount(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	mountPoint, _ := input["mount_point"].(string)
	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)
	snapshotID, _ := input["snapshot_id"].(string)

	if mountPoint == "" {
		return errorResponse(fmt.Errorf("mount_point is required")), nil
	}
	if repository == "" {
		repository = os.Getenv(repoEnvPrefix)
	}
	if password == "" {
		password = os.Getenv(passEnvPrefix)
		if password == "" {
			password = "backup-password"
		}
	}

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	args := []string{"mount", mountPoint}
	if snapshotID != "" {
		args = append(args, snapshotID)
	}

	cmd := exec.CommandContext(ctx, resticPath, args...)
	cmd.Env = append(os.Environ(), env...)

	log.Printf("Backup: mounting repository %s at %s (background)", repository, mountPoint)

	go func() {
		cmd.Run()
	}()

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"repository":  repository,
			"mount_point": mountPoint,
			"snapshot_id": snapshotID,
			"status":      "mounting",
		}),
	}, nil
}

func handleBackupCat(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	snapshotID, _ := input["snapshot_id"].(string)
	repository, _ := input["repository"].(string)
	password, _ := input["password"].(string)

	if snapshotID == "" {
		return errorResponse(fmt.Errorf("snapshot_id is required")), nil
	}
	if repository == "" {
		repository = os.Getenv(repoEnvPrefix)
	}
	if password == "" {
		password = os.Getenv(passEnvPrefix)
		if password == "" {
			password = "backup-password"
		}
	}

	env := []string{
		fmt.Sprintf("%s=%s", repoEnvPrefix, repository),
		fmt.Sprintf("%s=%s", passEnvPrefix, password),
	}

	cmd := exec.CommandContext(ctx, resticPath, "cat", "snapshots", snapshotID)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("restic cat failed: %w, output: %s", err, string(output))), nil
	}

	var snapshot map[string]interface{}
	json.Unmarshal(output, &snapshot)

	log.Printf("Backup: cat snapshot %s", snapshotID)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":         snapshotID,
			"snapshot":   snapshot,
			"repository": repository,
		}),
	}, nil
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "BACKUP_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
