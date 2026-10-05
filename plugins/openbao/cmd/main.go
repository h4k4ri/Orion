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
		ID:      "openbao-plugin",
		Name:    "OpenBao",
		Version: "1.0.0",
		Vendor:  "OpenBao",
	})

	p.Resource("orion.io/secret.secret", "v1").
		Handle("create", handleSecretCreate).
		Handle("read", handleSecretRead).
		Handle("update", handleSecretUpdate).
		Handle("delete", handleSecretDelete).
		Handle("list", handleSecretList).
		AddCapability("encryption", true).
		AddCapability("versioning", true).
		AddCapability("lease_renewal", true).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50057"
	}
	log.Printf("OpenBao plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func handleSecretCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	secret, _ := input["secret"].(map[string]interface{})

	if path == "" {
		return errorResponse(fmt.Errorf("path is required")), nil
	}
	if secret == nil {
		return errorResponse(fmt.Errorf("secret is required")), nil
	}

	secretJSON, _ := json.Marshal(secret)
	secretStr := string(secretJSON)

	cmd := exec.CommandContext(ctx, "bao", "write", "-f", path)
	cmd.Stdin = strings.NewReader(secretStr)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("OpenBao: write failed: %s, %v", string(output), err)
		return errorResponse(fmt.Errorf("bao write failed: %w", err)), nil
	}

	secretPath := fmt.Sprintf("secret/%s", path)
	log.Printf("OpenBao: created secret at %s", secretPath)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"path":   secretPath,
			"status": "created",
		}),
	}, nil
}

func handleSecretRead(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	if path == "" {
		return errorResponse(fmt.Errorf("path is required")), nil
	}

	fullPath := path
	if !strings.HasPrefix(path, "secret/") {
		fullPath = fmt.Sprintf("secret/%s", path)
	}

	cmd := exec.CommandContext(ctx, "bao", "read", "-field=data", "-format=json", fullPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("OpenBao: read failed: %s, %v", string(output), err)
		return errorResponse(fmt.Errorf("bao read failed: %w", err)), nil
	}

	var secret map[string]interface{}
	if err := json.Unmarshal(output, &secret); err != nil {
		secret = map[string]interface{}{"data": string(output)}
	}

	log.Printf("OpenBao: read secret at %s", fullPath)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"path":   fullPath,
			"secret": secret,
		}),
	}, nil
}

func handleSecretUpdate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	secret, _ := input["secret"].(map[string]interface{})

	if path == "" {
		return errorResponse(fmt.Errorf("path is required")), nil
	}

	secretJSON, _ := json.Marshal(secret)
	secretStr := string(secretJSON)

	cmd := exec.CommandContext(ctx, "bao", "write", path)
	cmd.Stdin = strings.NewReader(secretStr)

	_, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao write failed: %w", err)), nil
	}

	log.Printf("OpenBao: updated secret at %s", path)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"path":   path,
			"status": "updated",
		}),
	}, nil
}

func handleSecretDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	if path == "" {
		return errorResponse(fmt.Errorf("path is required")), nil
	}

	cmd := exec.CommandContext(ctx, "bao", "delete", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao delete failed: %w, %s", err, string(output))), nil
	}

	log.Printf("OpenBao: deleted secret at %s", path)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleSecretList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	path, _ := input["path"].(string)
	if path == "" {
		path = "secret/"
	}

	cmd := exec.CommandContext(ctx, "bao", "list", "-format=json", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao list failed: %w", err)), nil
	}

	var keys []string
	if err := json.Unmarshal(output, &keys); err != nil {
		keys = strings.Split(strings.TrimSpace(string(output)), "\n")
	}

	log.Printf("OpenBao: listed %d secrets at %s", len(keys), path)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"path": path,
			"keys": keys,
		}),
	}, nil
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "OPENBAO_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
