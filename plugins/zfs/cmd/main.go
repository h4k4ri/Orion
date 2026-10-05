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

func main() {
	p := plugin.New(plugin.Config{
		ID:      "zfs-plugin",
		Name:    "ZFS",
		Version: "1.0.0",
		Vendor:  "OpenZFS",
	})

	p.Resource("orion.io/storage.volume", "v1").
		Handle("create", handleVolumeCreate).
		Handle("delete", handleVolumeDelete).
		Handle("attach", handleVolumeAttach).
		Handle("resize", handleVolumeResize).
		Handle("snapshot", handleVolumeSnapshot).
		AddCapability("snapshots", true).
		AddCapability("compression", true).
		AddCapability("deduplication", true).
		AddCapability("encryption", true).
		Register()

	p.Resource("orion.io/storage.snapshot", "v1").
		Handle("create", handleSnapshotCreate).
		Handle("delete", handleSnapshotDelete).
		Handle("rollback", handleSnapshotRollback).
		Handle("clone", handleSnapshotClone).
		Register()

	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", handleAttachmentPrepare).
		Handle("release", handleAttachmentRelease).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50051"
	}
	log.Printf("ZFS plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func handleVolumeCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	size, _ := input["size"].(float64)
	pool, _ := input["pool"].(string)
	compression, _ := input["compression"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	if pool == "" {
		pool = "tank"
	}

	volumeName := fmt.Sprintf("%s/%s", pool, name)
	sizeGB := int(size)

	cmd := exec.CommandContext(ctx, "zfs", "create", "-V", fmt.Sprintf("%dG", sizeGB), "-o", fmt.Sprintf("compression=%s", compression), volumeName)
	if err := cmd.Run(); err != nil {
		return errorResponse(fmt.Errorf("zfs create failed: %w", err)), nil
	}

	log.Printf("ZFS: created volume %s (%dG)", volumeName, sizeGB)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": volumeName, "name": name, "pool": pool, "size": sizeGB, "status": "available"}),
	}, nil
}

func handleVolumeDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	volumeID, _ := input["id"].(string)
	if volumeID == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	cmd := exec.CommandContext(ctx, "zfs", "destroy", "-f", volumeID)
	if err := cmd.Run(); err != nil {
		return errorResponse(fmt.Errorf("zfs destroy failed: %w", err)), nil
	}

	log.Printf("ZFS: deleted volume %s", volumeID)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleVolumeAttach(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	volumeID, _ := input["volume_id"].(string)
	instanceID, _ := input["instance_id"].(string)

	log.Printf("ZFS: attaching volume %s to instance %s", volumeID, instanceID)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"volume_id": volumeID, "instance_id": instanceID, "device": "/dev/zvol/" + volumeID, "status": "attached"}),
	}, nil
}

func handleVolumeResize(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	volumeID, _ := input["id"].(string)
	size, _ := input["size"].(float64)
	sizeGB := int(size)

	cmd := exec.CommandContext(ctx, "zfs", "set", fmt.Sprintf("volsize=%dG", sizeGB), volumeID)
	if err := cmd.Run(); err != nil {
		return errorResponse(fmt.Errorf("zfs resize failed: %w", err)), nil
	}

	log.Printf("ZFS: resized volume %s to %dG", volumeID, sizeGB)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": volumeID, "size": sizeGB, "status": "available"}),
	}, nil
}

func handleVolumeSnapshot(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	volumeID, _ := input["volume_id"].(string)
	snapshotName, _ := input["snapshot_name"].(string)

	snapshotID := fmt.Sprintf("%s@%s", volumeID, snapshotName)
	cmd := exec.CommandContext(ctx, "zfs", "snapshot", snapshotID)
	if err := cmd.Run(); err != nil {
		return errorResponse(fmt.Errorf("zfs snapshot failed: %w", err)), nil
	}

	log.Printf("ZFS: created snapshot %s", snapshotID)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": snapshotID, "volume_id": volumeID, "name": snapshotName, "status": "available"}),
	}, nil
}

func handleSnapshotCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	volumeID, _ := input["volume_id"].(string)
	name, _ := input["name"].(string)

	snapshotID := fmt.Sprintf("%s@snap-%s", volumeID, name)
	cmd := exec.CommandContext(ctx, "zfs", "snapshot", snapshotID)
	if err := cmd.Run(); err != nil {
		return errorResponse(fmt.Errorf("zfs snapshot failed: %w", err)), nil
	}

	log.Printf("ZFS: created snapshot %s", snapshotID)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": snapshotID, "volume_id": volumeID, "name": name, "status": "available"}),
	}, nil
}

func handleSnapshotDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	snapshotID, _ := input["id"].(string)
	if snapshotID == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	cmd := exec.CommandContext(ctx, "zfs", "destroy", snapshotID)
	if err := cmd.Run(); err != nil {
		return errorResponse(fmt.Errorf("zfs destroy failed: %w", err)), nil
	}

	log.Printf("ZFS: deleted snapshot %s", snapshotID)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleSnapshotRollback(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	snapshotID, _ := input["snapshot_id"].(string)
	if snapshotID == "" {
		return errorResponse(fmt.Errorf("snapshot_id is required")), nil
	}

	cmd := exec.CommandContext(ctx, "zfs", "rollback", "-R", snapshotID)
	if err := cmd.Run(); err != nil {
		return errorResponse(fmt.Errorf("zfs rollback failed: %w", err)), nil
	}

	log.Printf("ZFS: rolled back to snapshot %s", snapshotID)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleSnapshotClone(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	snapshotID, _ := input["snapshot_id"].(string)
	cloneName, _ := input["clone_name"].(string)

	cloneID := strings.Replace(snapshotID, "@", "-clone-", 1)
	if cloneName != "" {
		parts := strings.Split(snapshotID, "@")
		cloneID = fmt.Sprintf("%s-clone-%s", parts[0], cloneName)
	}

	cmd := exec.CommandContext(ctx, "zfs", "clone", snapshotID, cloneID)
	if err := cmd.Run(); err != nil {
		return errorResponse(fmt.Errorf("zfs clone failed: %w", err)), nil
	}

	log.Printf("ZFS: cloned snapshot %s to %s", snapshotID, cloneID)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": cloneID, "source_snapshot": snapshotID, "status": "available"}),
	}, nil
}

func handleAttachmentPrepare(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	sourceID := req.SourceResourceID
	targetID := req.TargetResourceID

	log.Printf("ZFS: preparing attachment source=%s target=%s", sourceID, targetID)

	return &plugin.RelationshipResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"volume_id": sourceID, "instance_id": targetID, "mount_point": "/mnt/zfs", "fs_type": "zfs"}),
	}, nil
}

func handleAttachmentRelease(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	sourceID := req.SourceResourceID
	targetID := req.TargetResourceID

	log.Printf("ZFS: releasing attachment source=%s target=%s", sourceID, targetID)

	return &plugin.RelationshipResponse{Success: true}, nil
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "EXEC_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
