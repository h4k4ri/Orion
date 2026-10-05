package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/horizon/orion/sdk/go/plugin"
	pluginv1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

type shareRecord struct {
	ID        string                 `json:"id"`
	Backend   string                 `json:"backend"`
	ProjectID string                 `json:"project_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Result    map[string]interface{} `json:"result,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

type controller struct {
	mu        sync.RWMutex
	clients   map[string]*plugin.Client
	shares    map[string]shareRecord
	stateFile string
}

func newController() *controller {
	return &controller{clients: make(map[string]*plugin.Client), shares: make(map[string]shareRecord), stateFile: os.Getenv("ORION_MANILA_STATE_FILE")}
}

func main() {
	controller := newController()
	if err := controller.load(); err != nil {
		log.Fatalf("failed to load Manila state: %v", err)
	}
	p := plugin.New(plugin.Config{ID: "manila-plugin", Name: "Orion Share Control", Version: "1.0.0", Vendor: "Orion"})
	p.Resource("orion.io/storage.share", "v1").
		Handle("create", controller.create).
		Handle("get", controller.get).
		Handle("list", controller.list).
		Handle("delete", controller.delete).
		Handle("resize", controller.forward).
		Handle("attach", controller.forward).
		Handle("detach", controller.forward).
		Handle("create_snapshot", controller.forward).
		Handle("delete_snapshot", controller.forward).
		Handle("restore_snapshot", controller.forward).
		AddCapability("unified_control", true).
		AddCapability("multi_backend", true).
		Register()
	p.Relationship("orion.io/storage.attachment", "source").
		Handle("prepare", controller.forwardRelationship).
		Handle("release", controller.forwardRelationship).
		Register()
	endpoint := envOr("ORION_PLUGIN_ENDPOINT", ":50070")
	log.Printf("Manila share control plugin starting on %s", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Manila plugin stopped: %v", err)
	}
}

func (c *controller) create(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := decode(req.Payload, &input); err != nil {
		return failure(err), nil
	}
	backend := strings.ToLower(strings.TrimSpace(stringValue(input["backend"])))
	if backend == "" {
		backend = strings.ToLower(envOr("ORION_MANILA_DEFAULT_BACKEND", "cephfs"))
	}
	name := stringValue(input["name"])
	if name == "" {
		return failure(errors.New("name is required")), nil
	}
	result, err := c.invoke(ctx, backend, "create", input)
	if err != nil {
		return failure(err), nil
	}
	id := stringValue(result["id"])
	if id == "" {
		id = "share-" + uuid.NewString()
	}
	record := shareRecord{ID: id, Backend: backend, ProjectID: stringValue(input["project_id"]), Name: name, Result: result, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	c.mu.Lock()
	c.shares[id] = record
	persistErr := c.persistLocked()
	c.mu.Unlock()
	if persistErr != nil {
		return failure(persistErr), nil
	}
	return success(result), nil
}

func (c *controller) get(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := decode(req.Payload, &input); err != nil {
		return failure(err), nil
	}
	id := stringValue(input["id"])
	if id == "" {
		return failure(errors.New("id is required")), nil
	}
	c.mu.RLock()
	record, ok := c.shares[id]
	c.mu.RUnlock()
	if !ok {
		return failure(fmt.Errorf("share not found: %s", id)), nil
	}
	return success(record.Result), nil
}

func (c *controller) list(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	c.mu.RLock()
	items := make([]shareRecord, 0, len(c.shares))
	for _, record := range c.shares {
		items = append(items, record)
	}
	c.mu.RUnlock()
	return success(map[string]interface{}{"items": items, "total": len(items)}), nil
}

func (c *controller) delete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := decode(req.Payload, &input); err != nil {
		return failure(err), nil
	}
	id := stringValue(input["id"])
	if id == "" {
		return failure(errors.New("id is required")), nil
	}
	c.mu.RLock()
	record, ok := c.shares[id]
	c.mu.RUnlock()
	if !ok {
		return failure(fmt.Errorf("share not found: %s", id)), nil
	}
	if _, err := c.invoke(ctx, record.Backend, "delete", input); err != nil {
		return failure(err), nil
	}
	c.mu.Lock()
	delete(c.shares, id)
	persistErr := c.persistLocked()
	c.mu.Unlock()
	if persistErr != nil {
		return failure(persistErr), nil
	}
	return success(map[string]interface{}{"id": id, "status": "deleted"}), nil
}

func (c *controller) forward(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := decode(req.Payload, &input); err != nil {
		return failure(err), nil
	}
	backend := strings.ToLower(strings.TrimSpace(stringValue(input["backend"])))
	if backend == "" {
		id := stringValue(input["id"])
		if id == "" {
			id = stringValue(input["share_id"])
		}
		c.mu.RLock()
		if record, ok := c.shares[id]; ok {
			backend = record.Backend
		}
		c.mu.RUnlock()
	}
	if backend == "" {
		backend = strings.ToLower(envOr("ORION_MANILA_DEFAULT_BACKEND", "cephfs"))
	}
	result, err := c.invoke(ctx, backend, req.Operation, input)
	if err != nil {
		return failure(err), nil
	}
	return success(result), nil
}

func (c *controller) forwardRelationship(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	backend := strings.ToLower(envOr("ORION_MANILA_DEFAULT_BACKEND", "cephfs"))
	c.mu.RLock()
	if record, ok := c.shares[req.SourceResourceID]; ok {
		backend = record.Backend
	}
	c.mu.RUnlock()
	result, err := c.invokeRelationship(ctx, backend, req.Operation, req.SourceResourceID, req.TargetResourceID, req.Payload)
	if err != nil {
		return &plugin.RelationshipResponse{Success: false, Error: &plugin.Error{Code: "MANILA_PROVIDER_ERROR", Message: err.Error()}}, nil
	}
	return &plugin.RelationshipResponse{Success: true, Result: result}, nil
}

func (c *controller) invoke(ctx context.Context, backend, operation string, input map[string]interface{}) (map[string]interface{}, error) {
	client, err := c.client(ctx, backend)
	if err != nil {
		return nil, err
	}
	payload, err := structpb.NewStruct(input)
	if err != nil {
		return nil, err
	}
	response, err := client.Invoke(ctx, &pluginv1.InvokeRequest{RequestId: uuid.NewString(), IdempotencyKey: uuid.NewString(), ProviderId: backend, Resource: "orion.io/storage.share", ResourceVersion: "v1", Operation: operation, Payload: payload})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		if response.GetError() != nil {
			return nil, fmt.Errorf("%s: %s", response.GetError().GetCode(), response.GetError().GetMessage())
		}
		return nil, errors.New("share provider operation failed")
	}
	if response.GetResult() == nil {
		return map[string]interface{}{}, nil
	}
	return response.GetResult().AsMap(), nil
}

func (c *controller) invokeRelationship(ctx context.Context, backend, operation, source, target string, raw []byte) ([]byte, error) {
	client, err := c.client(ctx, backend)
	if err != nil {
		return nil, err
	}
	input := map[string]interface{}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
	}
	payload, err := structpb.NewStruct(input)
	if err != nil {
		return nil, err
	}
	response, err := client.Invoke(ctx, &pluginv1.InvokeRequest{RequestId: uuid.NewString(), IdempotencyKey: uuid.NewString(), ProviderId: backend, Resource: "orion.io/storage.attachment", ResourceVersion: "v1", Operation: operation, Payload: payload, Metadata: map[string]string{"source_resource_id": source, "target_resource_id": target}})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		if response.GetError() != nil {
			return nil, fmt.Errorf("%s: %s", response.GetError().GetCode(), response.GetError().GetMessage())
		}
		return nil, errors.New("attachment provider operation failed")
	}
	if response.GetResult() == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(response.GetResult().AsMap())
}

func (c *controller) client(ctx context.Context, backend string) (*plugin.Client, error) {
	endpoint := backendEndpoint(backend)
	if endpoint == "" {
		return nil, fmt.Errorf("no endpoint configured for Manila backend %s", backend)
	}
	c.mu.RLock()
	client := c.clients[backend]
	c.mu.RUnlock()
	if client != nil {
		return client, nil
	}
	created, err := plugin.NewClient(ctx, plugin.ClientConfig{Endpoint: endpoint, Timeout: 30 * time.Second, Retry: &plugin.RetryConfig{MaxAttempts: 3, InitialDelay: 100 * time.Millisecond, MaxDelay: time.Second}})
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if existing := c.clients[backend]; existing != nil {
		_ = created.Close()
		return existing, nil
	}
	c.clients[backend] = created
	c.mu.Unlock()
	return created, nil
}

func backendEndpoint(backend string) string {
	switch strings.ToLower(backend) {
	case "cephfs":
		return envOr("ORION_MANILA_CEPHFS_ENDPOINT", ":50056")
	case "nfs":
		return envOr("ORION_MANILA_NFS_ENDPOINT", ":50054")
	case "smb":
		return envOr("ORION_MANILA_SMB_ENDPOINT", ":50058")
	case "glusterfs":
		return envOr("ORION_MANILA_GLUSTERFS_ENDPOINT", ":50053")
	default:
		return ""
	}
}

func (c *controller) load() error {
	if strings.TrimSpace(c.stateFile) == "" {
		return nil
	}
	payload, err := os.ReadFile(c.stateFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return json.Unmarshal(payload, &c.shares)
}
func (c *controller) persistLocked() error {
	if strings.TrimSpace(c.stateFile) == "" {
		return nil
	}
	directory := filepath.Dir(c.stateFile)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(c.shares, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".orion-manila-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o640); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, c.stateFile)
}

func decode(payload []byte, target interface{}) error {
	if len(payload) == 0 {
		return nil
	}
	return json.Unmarshal(payload, target)
}
func stringValue(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
func success(value interface{}) *plugin.ResourceResponse {
	payload, _ := json.Marshal(value)
	return &plugin.ResourceResponse{Success: true, Result: payload}
}
func failure(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "MANILA_ERROR", Message: err.Error()}}
}
func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
