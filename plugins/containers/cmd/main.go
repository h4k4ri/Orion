package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/google/uuid"

	"github.com/horizon/orion/plugins/containers/driver"
	"github.com/horizon/orion/plugins/containers/container"
	"github.com/horizon/orion/sdk/go/plugin"
)

var (
	_containers = sync.Map{}
	_images     = sync.Map{}
	_driver     driver.Driver
)

func main() {
	backend := os.Getenv("ORION_CONTAINERS_BACKEND")
	if backend == "" {
		backend = "containerd"
	}

	var err error
	_driver, err = driver.NewDriver(backend, driver.Config{
		Address: os.Getenv("ORION_CONTAINERS_ADDRESS"),
	})
	if err != nil {
		log.Fatalf("Failed to create container driver: %v", err)
	}

	p := plugin.New(plugin.Config{ID: "containers-plugin", Name: "Orion Containers", Version: "1.0.0", Vendor: "Orion"})
	p.Resource("orion.io/containers.pod", "v1").
		Handle("create", handleCreate).
		Handle("get", handleGet).
		Handle("delete", handleDelete).
		Handle("start", handleStart).
		Handle("stop", handleStop).
		Handle("logs", handleLogs).
		Handle("list", handleList).
		Handle("exec", handleExec).
		Handle("list_images", handleListImages).
		Handle("pull_image", handlePullImage).
		Handle("delete_image", handleDeleteImage).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50075"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer _driver.Close()
	if err := p.Serve(ctx, endpoint); err != nil {
		log.Fatalf("containers plugin stopped: %v", err)
	}
}

func handleCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input container.CreateContainerInput
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	if input.Name == "" {
		input.Name = "container-" + uuid.New().String()[:8]
	}

	c, err := _driver.CreateContainer(ctx, &input)
	if err != nil {
		return errorResponse(err), nil
	}

	_containers.Store(c.ID, c)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(c),
	}, nil
}

func handleGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	json.Unmarshal(req.Payload, &input)

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	if c, ok := _containers.Load(id); ok {
		return &plugin.ResourceResponse{Success: true, Result: mustMarshal(c.(*container.Container))}, nil
	}

	c, err := _driver.GetContainer(ctx, id)
	if err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(c),
	}, nil
}

func handleDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	json.Unmarshal(req.Payload, &input)

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	if err := _driver.DeleteContainer(ctx, id); err != nil {
		return errorResponse(err), nil
	}

	_containers.Delete(id)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"id": id, "status": "deleted"}),
	}, nil
}

func handleStart(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	json.Unmarshal(req.Payload, &input)

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	if err := _driver.StartContainer(ctx, id); err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"id": id, "status": "started"}),
	}, nil
}

func handleStop(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	json.Unmarshal(req.Payload, &input)

	id := getString(input, "id")
	timeout := getInt(input, "timeout")

	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	if err := _driver.StopContainer(ctx, id, timeout); err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"id": id, "status": "stopped"}),
	}, nil
}

func handleLogs(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID   string `json:"id"`
		Tail int    `json:"tail"`
	}
	json.Unmarshal(req.Payload, &input)

	if input.ID == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	if input.Tail == 0 {
		input.Tail = 100
	}

	logs, err := _driver.LogsContainer(ctx, input.ID, driver.LogsOptions{
		Stdout: true,
		Stderr: true,
		Tail:   input.Tail,
	})
	if err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"id": input.ID, "logs": logs}),
	}, nil
}

func handleList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if req.Payload != nil {
		json.Unmarshal(req.Payload, &input)
	}

	opts := driver.ListOptions{
		All:       getBool(input, "all"),
		Namespace: getString(input, "namespace"),
	}

	containers, err := _driver.ListContainers(ctx, opts)
	if err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"items": containers, "total": len(containers)}),
	}, nil
}

func handleExec(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		ID   string   `json:"id"`
		Cmd  []string `json:"cmd"`
	}
	json.Unmarshal(req.Payload, &input)

	if input.ID == "" || len(input.Cmd) == 0 {
		return errorResponse(fmt.Errorf("id and cmd are required")), nil
	}

	output, err := _driver.ExecContainer(ctx, input.ID, input.Cmd)
	if err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"id": input.ID, "output": output}),
	}, nil
}

func handleListImages(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	images, err := _driver.ListImages(ctx, driver.ListOptions{})
	if err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"items": images, "total": len(images)}),
	}, nil
}

func handlePullImage(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	json.Unmarshal(req.Payload, &input)

	ref := getString(input, "ref")
	if ref == "" {
		return errorResponse(fmt.Errorf("ref is required")), nil
	}

	if err := _driver.PullImage(ctx, ref); err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"ref": ref, "status": "pulled"}),
	}, nil
}

func handleDeleteImage(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	json.Unmarshal(req.Payload, &input)

	ref := getString(input, "ref")
	if ref == "" {
		return errorResponse(fmt.Errorf("ref is required")), nil
	}

	if err := _driver.DeleteImage(ctx, ref); err != nil {
		return errorResponse(err), nil
	}

	_images.Delete(ref)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"ref": ref, "status": "deleted"}),
	}, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{
		Success: false,
		Error:   &plugin.Error{Code: "OPERATION_FAILED", Message: err.Error()},
	}
}

func mustMarshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
