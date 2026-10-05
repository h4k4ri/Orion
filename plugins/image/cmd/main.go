package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/horizon/orion/sdk/go/plugin"
)

var defaultPool = "/var/lib/libvirt/images"

func main() {
	p := plugin.New(plugin.Config{
		ID:      "image-plugin",
		Name:    "Image",
		Version: "1.0.0",
		Vendor:  "QEMU",
	})

	p.Resource("orion.io/image.image", "v1").
		Handle("create", handleImageCreate).
		Handle("delete", handleImageDelete).
		Handle("resize", handleImageResize).
		Handle("convert", handleImageConvert).
		Handle("snapshot", handleImageSnapshot).
		Handle("info", handleImageInfo).
		Handle("clone", handleImageClone).
		Handle("import", handleImageImport).
		Handle("export", handleImageExport).
		AddCapability("qcow2", true).
		AddCapability("raw", true).
		AddCapability("vmdk", true).
		AddCapability("vdi", true).
		AddCapability("compression", true).
		AddCapability("encryption", true).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50061"
	}
	log.Printf("Image plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func handleImageCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	format, _ := input["format"].(string)
	sizeGB, _ := input["size"].(float64)
	backingFile, _ := input["backing_file"].(string)
	encryption, _ := input["encryption"].(bool)
	compression, _ := input["compression"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	if format == "" {
		format = "qcow2"
	}
	if sizeGB == 0 {
		sizeGB = 10
	}

	sizeStr := fmt.Sprintf("%dG", int(sizeGB))
	path := fmt.Sprintf("%s/%s", defaultPool, name)

	args := []string{"create", "-f", format}

	if backingFile != "" {
		args = append(args, "-b", backingFile)
	}

	if encryption {
		args = append(args, "-o", "encryption=on")
	}

	if compression != "" {
		args = append(args, "-o", fmt.Sprintf("compression_type=%s", compression))
	}

	args = append(args, path, sizeStr)

	cmd := exec.CommandContext(ctx, "qemu-img", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("qemu-img create failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Image: created %s (%s, %s)", path, format, sizeStr)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":         path,
			"name":       name,
			"path":       path,
			"format":     format,
			"size":       int(sizeGB),
			"encryption": encryption,
			"status":     "created",
		}),
	}, nil
}

func handleImageDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	if path == "" {
		return errorResponse(fmt.Errorf("path is required")), nil
	}

	cmd := exec.CommandContext(ctx, "rm", "-f", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("rm failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Image: deleted %s", path)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleImageResize(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	sizeGB, _ := input["size"].(float64)

	if path == "" {
		return errorResponse(fmt.Errorf("path is required")), nil
	}
	if sizeGB == 0 {
		return errorResponse(fmt.Errorf("size is required")), nil
	}

	sizeStr := fmt.Sprintf("%dG", int(sizeGB))
	cmd := exec.CommandContext(ctx, "qemu-img", "resize", path, sizeStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("qemu-img resize failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Image: resized %s to %s", path, sizeStr)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     path,
			"path":   path,
			"size":   int(sizeGB),
			"status": "resized",
		}),
	}, nil
}

func handleImageConvert(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	srcPath, _ := input["source_path"].(string)
	dstPath, _ := input["dest_path"].(string)
	srcFormat, _ := input["source_format"].(string)
	dstFormat, _ := input["dest_format"].(string)
	compression, _ := input["compression"].(string)

	if srcPath == "" || dstPath == "" {
		return errorResponse(fmt.Errorf("source_path and dest_path are required")), nil
	}
	if dstFormat == "" {
		dstFormat = "qcow2"
	}

	args := []string{"convert", "-O", dstFormat}

	if compression != "" {
		if compression == "lzo" || compression == "zlib" || compression == "zstd" {
			args = append(args, "-o", fmt.Sprintf("compression_type=%s", compression))
		}
	}

	if srcFormat != "" {
		args = append(args, "-f", srcFormat)
	}

	args = append(args, srcPath, dstPath)

	cmd := exec.CommandContext(ctx, "qemu-img", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("qemu-img convert failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Image: converted %s (%s) -> %s (%s)", srcPath, srcFormat, dstPath, dstFormat)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":            dstPath,
			"source_path":   srcPath,
			"dest_path":     dstPath,
			"source_format": srcFormat,
			"dest_format":   dstFormat,
			"status":        "converted",
		}),
	}, nil
}

func handleImageSnapshot(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	snapshotName, _ := input["snapshot_name"].(string)

	if path == "" || snapshotName == "" {
		return errorResponse(fmt.Errorf("path and snapshot_name are required")), nil
	}

	cmd := exec.CommandContext(ctx, "qemu-img", "snapshot", "-c", snapshotName, path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("qemu-img snapshot failed: %w, output: %s", err, string(output))), nil
	}

	snapshotID := fmt.Sprintf("%s@snapshot-%s", path, snapshotName)
	log.Printf("Image: created snapshot %s", snapshotID)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":            snapshotID,
			"path":          path,
			"snapshot_name": snapshotName,
			"status":        "snapshotted",
		}),
	}, nil
}

func handleImageInfo(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	if path == "" {
		return errorResponse(fmt.Errorf("path is required")), nil
	}

	cmd := exec.CommandContext(ctx, "qemu-img", "info", "--output", "json", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		cmd = exec.CommandContext(ctx, "qemu-img", "info", path)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return errorResponse(fmt.Errorf("qemu-img info failed: %w, output: %s", err, string(output))), nil
		}

		info := parseQemuImgInfo(string(output))
		log.Printf("Image: info for %s: %+v", path, info)

		return &plugin.ResourceResponse{
			Success: true,
			Result:  marshal(info),
		}, nil
	}

	var info map[string]interface{}
	if err := json.Unmarshal(output, &info); err != nil {
		return errorResponse(fmt.Errorf("failed to parse qemu-img output: %w", err)), nil
	}

	log.Printf("Image: info for %s", path)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(info),
	}, nil
}

func parseQemuImgInfo(output string) map[string]interface{} {
	info := make(map[string]interface{})
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			info[key] = val
		}
	}
	return info
}

func handleImageClone(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	sourcePath, _ := input["source_path"].(string)
	cloneName, _ := input["clone_name"].(string)
	format, _ := input["format"].(string)

	if sourcePath == "" || cloneName == "" {
		return errorResponse(fmt.Errorf("source_path and clone_name are required")), nil
	}
	if format == "" {
		format = "qcow2"
	}

	clonePath := fmt.Sprintf("%s/%s", defaultPool, cloneName)

	cmd := exec.CommandContext(ctx, "qemu-img", "create", "-f", format, "-b", sourcePath, clonePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("qemu-img clone failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Image: cloned %s -> %s", sourcePath, clonePath)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     clonePath,
			"name":   cloneName,
			"source": sourcePath,
			"path":   clonePath,
			"format": format,
			"status": "cloned",
		}),
	}, nil
}

func handleImageImport(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	url, _ := input["url"].(string)
	name, _ := input["name"].(string)
	format, _ := input["format"].(string)

	if url == "" {
		return errorResponse(fmt.Errorf("url is required")), nil
	}
	if name == "" {
		parts := strings.Split(url, "/")
		name = parts[len(parts)-1]
	}
	if format == "" {
		format = "qcow2"
	}

	destPath := fmt.Sprintf("%s/%s", defaultPool, name)

	cmd := exec.CommandContext(ctx, "curl", "-sL", url, "-o", destPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("curl failed: %w, output: %s", err, string(output))), nil
	}

	cmd = exec.CommandContext(ctx, "qemu-img", "convert", "-O", format, destPath, destPath+".qcow2")
	output, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("Image: convert note: %v", err)
	}

	log.Printf("Image: imported %s to %s", url, destPath)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     destPath,
			"name":   name,
			"url":    url,
			"path":   destPath,
			"format": format,
			"status": "imported",
		}),
	}, nil
}

func handleImageExport(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	destURL, _ := input["dest_url"].(string)
	format, _ := input["format"].(string)

	if path == "" || destURL == "" {
		return errorResponse(fmt.Errorf("path and dest_url are required")), nil
	}

	cmd := exec.CommandContext(ctx, "curl", "-sL", "-T", path, destURL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("curl upload failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("Image: exported %s to %s", path, destURL)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":       destURL,
			"path":     path,
			"dest_url": destURL,
			"format":   format,
			"status":   "exported",
		}),
	}, nil
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "IMAGE_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
func parseSizeGB(sizeStr string) int {
	sizeStr = strings.TrimSpace(sizeStr)
	if strings.HasSuffix(sizeStr, "G") {
		size, _ := strconv.Atoi(strings.TrimSuffix(sizeStr, "G"))
		return size
	}
	if strings.HasSuffix(sizeStr, "M") {
		size, _ := strconv.Atoi(strings.TrimSuffix(sizeStr, "M"))
		return size / 1024
	}
	if strings.HasSuffix(sizeStr, "T") {
		size, _ := strconv.Atoi(strings.TrimSuffix(sizeStr, "T"))
		return size * 1024
	}
	size, _ := strconv.Atoi(sizeStr)
	return size / (1024 * 1024 * 1024)
}
