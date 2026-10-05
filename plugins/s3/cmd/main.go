package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/horizon/orion/sdk/go/plugin"
)

type s3Config struct {
	endpoint  string
	accessKey string
	secretKey string
	region    string
	bucket    string
}

func main() {
	p := plugin.New(plugin.Config{
		ID:      "s3-plugin",
		Name:    "S3",
		Version: "1.0.0",
		Vendor:  "MinIO/AWS/Ceph",
	})

	p.Resource("orion.io/storage.object", "v1").
		Handle("create_bucket", handleBucketCreate).
		Handle("delete_bucket", handleBucketDelete).
		Handle("list_buckets", handleBucketList).
		Handle("put_object", handleObjectPut).
		Handle("get_object", handleObjectGet).
		Handle("delete_object", handleObjectDelete).
		Handle("list_objects", handleObjectList).
		Handle("copy_object", handleObjectCopy).
		Handle("get_object_metadata", handleObjectMetadata).
		Handle("set_versioning", handleSetVersioning).
		Handle("set_lifecycle", handleSetLifecycle).
		Handle("set_cors", handleSetCORS).
		Handle("set_encryption", handleSetEncryption).
		AddCapability("versioning", true).
		AddCapability("lifecycle", true).
		AddCapability("cors", true).
		AddCapability("encryption", true).
		AddCapability("swift_compatible", true).
		Register()
	p.Resource("orion.io/storage.object.swift", "v1").
		Handle("create_bucket", handleBucketCreate).
		Handle("delete_bucket", handleBucketDelete).
		Handle("list_buckets", handleBucketList).
		Handle("put_object", handleObjectPut).
		Handle("get_object", handleObjectGet).
		Handle("delete_object", handleObjectDelete).
		Handle("list_objects", handleObjectList).
		Handle("copy_object", handleObjectCopy).
		Handle("get_object_metadata", handleObjectMetadata).
		Handle("set_versioning", handleSetVersioning).
		Handle("set_lifecycle", handleSetLifecycle).
		Handle("set_cors", handleSetCORS).
		Handle("set_encryption", handleSetEncryption).
		AddCapability("swift_compatibility", true).
		AddCapability("cors", true).
		AddCapability("encryption", true).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50060"
	}
	log.Printf("S3 plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func getS3Cmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "mc", args...)
	return cmd
}

func handleBucketCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	region, _ := input["region"].(string)
	endpoint, _ := input["endpoint"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	alias, err := ensureAlias(ctx, endpoint, stringValue(input["alias"]))
	if err != nil {
		return errorResponse(err), nil
	}

	cmd := getS3Cmd(ctx, "mb", fmt.Sprintf("%s/%s", alias, name))
	if region != "" {
		cmd = getS3Cmd(ctx, "mb", fmt.Sprintf("%s/%s", alias, name), "--region", region)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc mb failed: %w, output: %s", err, string(output))), nil
	}

	bucketID := fmt.Sprintf("%s/%s", alias, name)
	log.Printf("S3: created bucket %s", bucketID)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     bucketID,
			"name":   name,
			"region": region,
			"status": "created",
		}),
	}, nil
}

func handleBucketDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	bucketID, _ := input["id"].(string)
	if bucketID == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	cmd := getS3Cmd(ctx, "rb", bucketID, "--force")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc rb failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("S3: deleted bucket %s", bucketID)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleBucketList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if len(req.Payload) > 0 {
		if err := json.Unmarshal(req.Payload, &input); err != nil {
			return errorResponse(err), nil
		}
	}
	alias, err := storageAlias(ctx, input)
	if err != nil {
		return errorResponse(err), nil
	}
	cmd := getS3Cmd(ctx, "ls", alias+"/")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc ls failed: %w, output: %s", err, string(output))), nil
	}

	lines := strings.Split(string(output), "\n")
	var buckets []map[string]interface{}
	for _, line := range lines {
		if strings.Contains(line, alias+"/") {
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				buckets = append(buckets, map[string]interface{}{
					"name":    strings.TrimPrefix(parts[len(parts)-1], alias+"/"),
					"size":    parts[2],
					"created": parts[0],
				})
			}
		}
	}

	log.Printf("S3: listed %d buckets", len(buckets))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"buckets": buckets,
		}),
	}, nil
}

func handleObjectPut(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	bucket, _ := input["bucket"].(string)
	key, _ := input["key"].(string)
	sourceFile, _ := input["source_file"].(string)
	contentType, _ := input["content_type"].(string)
	storageClass, _ := input["storage_class"].(string)
	body, _ := input["body"].(string)

	if bucket == "" || key == "" {
		return errorResponse(fmt.Errorf("bucket and key are required")), nil
	}
	alias, err := storageAlias(ctx, input)
	if err != nil {
		return errorResponse(err), nil
	}

	args := []string{"cp"}
	if storageClass != "" {
		args = append(args, "--attr", fmt.Sprintf("x-amz-storage-class=%s", storageClass))
	}
	if contentType != "" {
		args = append(args, "--attr", fmt.Sprintf("Content-Type=%s", contentType))
	}

	if sourceFile != "" {
		args = append(args, sourceFile)
	} else if body != "" {
		args = append(args, "/dev/stdin")
	}

	args = append(args, fmt.Sprintf("%s/%s/%s", alias, bucket, key))

	cmd := getS3Cmd(ctx, args...)
	var output []byte
	if sourceFile == "" && body != "" {
		cmd.Stdin = strings.NewReader(body)
		output, err = cmd.CombinedOutput()
	} else {
		output, err = cmd.CombinedOutput()
	}

	if err != nil {
		return errorResponse(fmt.Errorf("mc cp failed: %w, output: %s", err, string(output))), nil
	}

	objectID := fmt.Sprintf("%s/%s", bucket, key)
	log.Printf("S3: uploaded object %s", objectID)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":           objectID,
			"bucket":       bucket,
			"key":          key,
			"status":       "uploaded",
			"content_type": contentType,
		}),
	}, nil
}

func handleObjectGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	bucket, _ := input["bucket"].(string)
	key, _ := input["key"].(string)
	destFile, _ := input["dest_file"].(string)

	if bucket == "" || key == "" {
		return errorResponse(fmt.Errorf("bucket and key are required")), nil
	}

	alias, err := storageAlias(ctx, input)
	if err != nil {
		return errorResponse(err), nil
	}
	source := fmt.Sprintf("%s/%s/%s", alias, bucket, key)
	var args []string
	if destFile != "" {
		args = []string{"cp", source, destFile}
	} else {
		args = []string{"cat", source}
	}

	cmd := getS3Cmd(ctx, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc get failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("S3: got object %s/%s", bucket, key)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"bucket": bucket,
			"key":    key,
			"size":   len(output),
			"data":   string(output),
		}),
	}, nil
}

func handleObjectDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	bucket, _ := input["bucket"].(string)
	key, _ := input["key"].(string)
	versionId, _ := input["version_id"].(string)

	if bucket == "" || key == "" {
		return errorResponse(fmt.Errorf("bucket and key are required")), nil
	}

	alias, err := storageAlias(ctx, input)
	if err != nil {
		return errorResponse(err), nil
	}
	objectPath := fmt.Sprintf("%s/%s/%s", alias, bucket, key)
	if versionId != "" {
		objectPath = fmt.Sprintf("%s?versionId=%s", objectPath, versionId)
	}

	cmd := getS3Cmd(ctx, "rm", objectPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc rm failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("S3: deleted object %s/%s", bucket, key)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleObjectList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	bucket, _ := input["bucket"].(string)
	prefix, _ := input["prefix"].(string)

	if bucket == "" {
		return errorResponse(fmt.Errorf("bucket is required")), nil
	}
	alias, err := storageAlias(ctx, input)
	if err != nil {
		return errorResponse(err), nil
	}

	path := fmt.Sprintf("%s/%s", alias, bucket)
	if prefix != "" {
		path = fmt.Sprintf("%s/%s/%s", alias, bucket, prefix)
	}

	cmd := getS3Cmd(ctx, "ls", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc ls failed: %w, output: %s", err, string(output))), nil
	}

	lines := strings.Split(string(output), "\n")
	var objects []map[string]interface{}
	for _, line := range lines {
		if strings.Contains(line, alias+"/") {
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				fullPath := parts[len(parts)-1]
				objKey := strings.TrimPrefix(fullPath, fmt.Sprintf("%s/%s/", alias, bucket))
				objects = append(objects, map[string]interface{}{
					"bucket":        bucket,
					"key":           objKey,
					"size":          parts[2],
					"last_modified": parts[0] + " " + parts[1],
				})
			}
		}
	}

	log.Printf("S3: listed %d objects in bucket %s", len(objects), bucket)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"bucket":  bucket,
			"objects": objects,
		}),
	}, nil
}

func handleObjectCopy(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	srcBucket, _ := input["src_bucket"].(string)
	srcKey, _ := input["src_key"].(string)
	dstBucket, _ := input["dst_bucket"].(string)
	dstKey, _ := input["dst_key"].(string)

	if srcBucket == "" || srcKey == "" || dstBucket == "" || dstKey == "" {
		return errorResponse(fmt.Errorf("src_bucket, src_key, dst_bucket, and dst_key are required")), nil
	}
	alias, err := storageAlias(ctx, input)
	if err != nil {
		return errorResponse(err), nil
	}

	src := fmt.Sprintf("%s/%s/%s", alias, srcBucket, srcKey)
	dst := fmt.Sprintf("%s/%s/%s", alias, dstBucket, dstKey)

	cmd := getS3Cmd(ctx, "cp", src, dst)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc cp failed: %w, output: %s", err, string(output))), nil
	}

	objectID := fmt.Sprintf("%s/%s", dstBucket, dstKey)
	log.Printf("S3: copied object to %s", objectID)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":         objectID,
			"src_bucket": srcBucket,
			"src_key":    srcKey,
			"dst_bucket": dstBucket,
			"dst_key":    dstKey,
		}),
	}, nil
}

func handleObjectMetadata(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	bucket, _ := input["bucket"].(string)
	key, _ := input["key"].(string)

	if bucket == "" || key == "" {
		return errorResponse(fmt.Errorf("bucket and key are required")), nil
	}
	alias, err := storageAlias(ctx, input)
	if err != nil {
		return errorResponse(err), nil
	}

	cmd := getS3Cmd(ctx, "stat", fmt.Sprintf("%s/%s/%s", alias, bucket, key))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc stat failed: %w, output: %s", err, string(output))), nil
	}

	lines := strings.Split(string(output), "\n")
	metadata := make(map[string]string)
	for _, line := range lines {
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				metadata[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	log.Printf("S3: got metadata for %s/%s", bucket, key)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"bucket":   bucket,
			"key":      key,
			"metadata": metadata,
		}),
	}, nil
}

func handleSetVersioning(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Bucket   string `json:"bucket"`
		Enabled  bool   `json:"enabled"`
		Endpoint string `json:"endpoint,omitempty"`
		Alias    string `json:"alias,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Bucket == "" {
		return errorResponse(fmt.Errorf("bucket is required")), nil
	}
	args := []string{"version", "enable"}
	if !input.Enabled {
		args = []string{"version", "suspend"}
	}
	alias, err := storageAlias(ctx, map[string]interface{}{"endpoint": input.Endpoint, "alias": input.Alias})
	if err != nil {
		return errorResponse(err), nil
	}
	args = append(args, alias+"/"+input.Bucket)
	output, err := getS3Cmd(ctx, args...).CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc version operation failed: %w, output: %s", err, string(output))), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: marshal(map[string]interface{}{"bucket": input.Bucket, "enabled": input.Enabled})}, nil
}

func handleSetLifecycle(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Bucket        string                   `json:"bucket"`
		Configuration json.RawMessage          `json:"configuration"`
		Rules         []map[string]interface{} `json:"rules"`
		Endpoint      string                   `json:"endpoint,omitempty"`
		Alias         string                   `json:"alias,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Bucket == "" {
		return errorResponse(fmt.Errorf("bucket is required")), nil
	}
	configuration := input.Configuration
	if len(configuration) == 0 {
		var err error
		configuration, err = json.Marshal(map[string]interface{}{"Rules": input.Rules})
		if err != nil {
			return errorResponse(err), nil
		}
	}
	alias, err := storageAlias(ctx, map[string]interface{}{"endpoint": input.Endpoint, "alias": input.Alias})
	if err != nil {
		return errorResponse(err), nil
	}
	cmd := getS3Cmd(ctx, "ilm", "import", alias+"/"+input.Bucket)
	cmd.Stdin = strings.NewReader(string(configuration))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc ilm import failed: %w, output: %s", err, string(output))), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: marshal(map[string]interface{}{"bucket": input.Bucket, "configuration": json.RawMessage(configuration)})}, nil
}

func handleSetCORS(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Bucket        string          `json:"bucket"`
		Configuration json.RawMessage `json:"configuration"`
		Endpoint      string          `json:"endpoint,omitempty"`
		Alias         string          `json:"alias,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Bucket == "" || len(input.Configuration) == 0 {
		return errorResponse(fmt.Errorf("bucket and configuration are required")), nil
	}
	var raw interface{}
	if err := json.Unmarshal(input.Configuration, &raw); err != nil {
		return errorResponse(fmt.Errorf("invalid CORS configuration: %w", err)), nil
	}
	alias, err := storageAlias(ctx, map[string]interface{}{"endpoint": input.Endpoint, "alias": input.Alias})
	if err != nil {
		return errorResponse(err), nil
	}
	target := fmt.Sprintf("%s/%s", alias, input.Bucket)
	file, err := os.CreateTemp("", "orion-cors-*.json")
	if err != nil {
		return errorResponse(err), nil
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(input.Configuration); err != nil {
		_ = file.Close()
		return errorResponse(err), nil
	}
	if err := file.Close(); err != nil {
		return errorResponse(err), nil
	}
	output, err := getS3Cmd(ctx, "cors", "set", target, name).CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc cors set failed: %w, output: %s", err, string(output))), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: marshal(map[string]interface{}{"bucket": input.Bucket, "configuration": raw})}, nil
}

func handleSetEncryption(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Bucket   string `json:"bucket"`
		Mode     string `json:"mode"`
		Key      string `json:"key"`
		Enable   *bool  `json:"enabled"`
		Endpoint string `json:"endpoint,omitempty"`
		Alias    string `json:"alias,omitempty"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if input.Bucket == "" {
		return errorResponse(fmt.Errorf("bucket is required")), nil
	}
	alias, err := storageAlias(ctx, map[string]interface{}{"endpoint": input.Endpoint, "alias": input.Alias})
	if err != nil {
		return errorResponse(err), nil
	}
	target := alias + "/" + input.Bucket
	args := []string{"encrypt", "set"}
	enabled := input.Enable == nil || *input.Enable
	if !enabled {
		args = []string{"encrypt", "clear"}
	} else {
		mode := strings.ToLower(input.Mode)
		if mode == "" {
			mode = "sse-s3"
		}
		args = append(args, mode)
		if input.Key != "" {
			args = append(args, input.Key)
		}
	}
	args = append(args, target)
	output, err := getS3Cmd(ctx, args...).CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("mc encryption operation failed: %w, output: %s", err, string(output))), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: marshal(map[string]interface{}{"bucket": input.Bucket, "enabled": enabled, "mode": input.Mode})}, nil
}

func storageAlias(ctx context.Context, input map[string]interface{}) (string, error) {
	if input == nil {
		input = map[string]interface{}{}
	}
	endpoint := stringValue(input["endpoint"])
	return ensureAlias(ctx, endpoint, stringValue(input["alias"]))
}

func ensureAlias(ctx context.Context, endpoint, requested string) (string, error) {
	alias := requested
	if alias == "" {
		alias = getEnvOr("ORION_S3_ALIAS", "myminio")
	}
	if endpoint == "" {
		endpoint = os.Getenv("ORION_S3_ENDPOINT")
	}
	if endpoint == "" {
		return alias, nil
	}
	if requested == "" {
		parsed, err := url.Parse(endpoint)
		if err == nil && parsed.Hostname() != "" {
			alias = "orion-" + strings.NewReplacer(".", "-", ":", "-", "/", "-").Replace(parsed.Hostname())
		}
	}
	output, err := getS3Cmd(ctx, "alias", "set", alias, endpoint, getEnvOr("AWS_ACCESS_KEY_ID", "minioadmin"), getEnvOr("AWS_SECRET_ACCESS_KEY", "minioadmin")).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mc alias set failed: %w, output: %s", err, string(output))
	}
	return alias, nil
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}

func getEnvOr(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "S3_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
