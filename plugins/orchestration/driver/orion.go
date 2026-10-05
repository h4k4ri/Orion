package driver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/horizon/orion/plugins/orchestration/stack"
	"github.com/horizon/orion/sdk/go/plugin"
	pluginv1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

// OrionDriver materializes declarative resources through the Orion plugin
// protocol. Providers stay independently deployable; this driver owns graph
// ordering, resource state and replacement semantics.
type OrionDriver struct {
	mu      sync.Mutex
	clients map[string]*plugin.Client
}

type nativeDocument struct{ Resources map[string]nativeResource }

type nativeResource struct {
	Type         string
	Properties   map[string]interface{}
	DependsOn    []string
	ProviderName string
	ProviderURL  string
}

func NewOrionDriver(_ Config) (Driver, error) {
	return &OrionDriver{clients: make(map[string]*plugin.Client)}, nil
}

func (d *OrionDriver) Create(ctx context.Context, st *stack.Stack, template *stack.Template) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	doc, order, err := parseNativeTemplate(template)
	if err != nil {
		return err
	}
	if st.Outputs == nil {
		st.Outputs = make(map[string]interface{})
	}
	created := make([]string, 0, len(order))
	for _, name := range order {
		resource := doc.Resources[name]
		result, invokeErr := d.invoke(ctx, st, name, resource, "create", nil)
		if invokeErr != nil {
			for index := len(created) - 1; index >= 0; index-- {
				createdName := created[index]
				createdResource := doc.Resources[createdName]
				_, _ = d.invoke(ctx, st, createdName, createdResource, "delete", resourceState(st, createdName))
			}
			return fmt.Errorf("create resource %s: %w", name, invokeErr)
		}
		st.Outputs[name] = result
		created = append(created, name)
	}
	st.UpdatedAt = time.Now().UTC()
	return nil
}

func (d *OrionDriver) Update(ctx context.Context, st *stack.Stack, template *stack.Template) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	doc, order, err := parseNativeTemplate(template)
	if err != nil {
		return err
	}
	if st.Outputs == nil {
		st.Outputs = make(map[string]interface{})
	}
	for _, name := range order {
		resource := doc.Resources[name]
		state := resourceState(st, name)
		var result map[string]interface{}
		if state == nil {
			result, err = d.invoke(ctx, st, name, resource, "create", nil)
		} else {
			result, err = d.invoke(ctx, st, name, resource, "update", state)
			if err != nil {
				if _, deleteErr := d.invoke(ctx, st, name, resource, "delete", state); deleteErr != nil {
					return fmt.Errorf("replace resource %s: delete: %w", name, deleteErr)
				}
				result, err = d.invoke(ctx, st, name, resource, "create", nil)
			}
		}
		if err != nil {
			return fmt.Errorf("update resource %s: %w", name, err)
		}
		st.Outputs[name] = result
	}
	known := make(map[string]struct{}, len(order))
	for _, name := range order {
		known[name] = struct{}{}
	}
	metadata, _ := st.Outputs["_resources"].(map[string]interface{})
	for name, raw := range metadata {
		if _, ok := known[name]; ok {
			continue
		}
		definition, _ := raw.(map[string]interface{})
		resource := nativeResource{Type: stringValue(definition["type"]), ProviderName: stringValue(definition["provider"]), ProviderURL: stringValue(definition["endpoint"])}
		if _, err := d.invoke(ctx, st, name, resource, "delete", resourceState(st, name)); err != nil {
			return fmt.Errorf("delete removed resource %s: %w", name, err)
		}
		delete(st.Outputs, name)
		delete(metadata, name)
	}
	st.UpdatedAt = time.Now().UTC()
	return nil
}

func (d *OrionDriver) Delete(ctx context.Context, st *stack.Stack) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if st.Outputs == nil {
		return nil
	}
	metadata, _ := st.Outputs["_resources"].(map[string]interface{})
	if len(metadata) == 0 {
		return nil
	}
	names := make([]string, 0, len(metadata))
	for name := range metadata {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		definition, _ := metadata[name].(map[string]interface{})
		resource := nativeResource{Type: stringValue(definition["type"]), ProviderName: stringValue(definition["provider"]), ProviderURL: stringValue(definition["endpoint"])}
		if _, err := d.invoke(ctx, st, name, resource, "delete", resourceState(st, name)); err != nil {
			return fmt.Errorf("delete resource %s: %w", name, err)
		}
		delete(st.Outputs, name)
	}
	delete(st.Outputs, "_resources")
	st.UpdatedAt = time.Now().UTC()
	return nil
}

func (d *OrionDriver) Suspend(context.Context, *stack.Stack) error   { return nil }
func (d *OrionDriver) Resume(context.Context, *stack.Stack) error    { return nil }
func (d *OrionDriver) Check(_ context.Context, _ *stack.Stack) error { return nil }

func (d *OrionDriver) GetOutputs(_ context.Context, st *stack.Stack) (map[string]interface{}, error) {
	if st.Outputs == nil {
		return map[string]interface{}{}, nil
	}
	return st.Outputs, nil
}

func (d *OrionDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, client := range d.clients {
		_ = client.Close()
		delete(d.clients, key)
	}
	return nil
}

func parseNativeTemplate(template *stack.Template) (nativeDocument, []string, error) {
	if template == nil || strings.TrimSpace(template.Content) == "" {
		return nativeDocument{}, nil, errors.New("orion backend requires a YAML/Heat template")
	}
	var document map[string]interface{}
	if err := yaml.Unmarshal([]byte(template.Content), &document); err != nil {
		return nativeDocument{}, nil, fmt.Errorf("decode Orion template: %w", err)
	}
	rawResources := document["resources"]
	if rawResources == nil {
		rawResources = document["Resources"]
	}
	resources, ok := rawResources.(map[string]interface{})
	if !ok || len(resources) == 0 {
		return nativeDocument{}, nil, errors.New("Orion template requires a non-empty resources object")
	}
	doc := nativeDocument{Resources: make(map[string]nativeResource, len(resources))}
	for name, raw := range resources {
		definition, ok := raw.(map[string]interface{})
		if !ok {
			return nativeDocument{}, nil, fmt.Errorf("resource %s must be an object", name)
		}
		typ := stringValue(firstValue(definition, "type", "Type"))
		if typ == "" {
			return nativeDocument{}, nil, fmt.Errorf("resource %s has no type", name)
		}
		doc.Resources[name] = nativeResource{
			Type:         typ,
			Properties:   mapValue(firstValue(definition, "properties", "Properties")),
			DependsOn:    stringList(firstValue(definition, "depends_on", "DependsOn")),
			ProviderName: stringValue(firstValue(definition, "provider", "Provider")),
			ProviderURL:  stringValue(firstValue(definition, "provider_endpoint", "ProviderEndpoint", "endpoint", "Endpoint")),
		}
	}
	order, err := dependencyOrder(doc.Resources)
	return doc, order, err
}

// ValidateNativeTemplate exposes the same parser used by Create/Update so
// preview and API validation cannot drift from the execution path.
func ValidateNativeTemplate(template *stack.Template) error {
	_, _, err := parseNativeTemplate(template)
	return err
}

func dependencyOrder(resources map[string]nativeResource) ([]string, error) {
	state := make(map[string]int, len(resources))
	order := make([]string, 0, len(resources))
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	sort.Strings(names)
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("resource dependency cycle at %s", name)
		}
		if state[name] == 2 {
			return nil
		}
		resource, ok := resources[name]
		if !ok {
			return fmt.Errorf("resource depends on unknown resource %s", name)
		}
		state[name] = 1
		for _, dependency := range resource.DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[name] = 2
		order = append(order, name)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (d *OrionDriver) invoke(ctx context.Context, st *stack.Stack, name string, resource nativeResource, operation string, state map[string]interface{}) (map[string]interface{}, error) {
	if resource.Type == "" {
		return nil, errors.New("resource type is required")
	}
	endpoint := providerEndpoint(resource.Type, resource.ProviderName, resource.ProviderURL)
	if endpoint == "" {
		return nil, fmt.Errorf("no endpoint configured for provider %s", resource.Type)
	}
	client, err := d.client(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	payload := make(map[string]interface{}, len(resource.Properties)+8)
	for key, value := range resource.Properties {
		payload[key] = value
	}
	payload["name"] = stringOr(payload["name"], name)
	payload["stack_id"] = st.ID
	payload["project_id"] = st.ProjectID
	payload["tenant_id"] = st.TenantID
	if state != nil {
		if id := stringValue(state["id"]); id != "" {
			payload["id"] = id
		}
	}
	structPayload, err := structpb.NewStruct(payload)
	if err != nil {
		return nil, fmt.Errorf("encode resource %s: %w", name, err)
	}
	response, err := client.Invoke(ctx, &pluginv1.InvokeRequest{RequestId: uuid.NewString(), IdempotencyKey: uuid.NewString(), ProviderId: endpoint, Resource: resource.Type, ResourceVersion: "v1", Operation: operation, Payload: structPayload})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		if response.GetError() != nil {
			return nil, fmt.Errorf("%s: %s", response.GetError().GetCode(), response.GetError().GetMessage())
		}
		return nil, fmt.Errorf("provider rejected %s", operation)
	}
	result := map[string]interface{}{}
	if response.GetResult() != nil {
		result = response.GetResult().AsMap()
	}
	if operation == "create" && stringValue(result["id"]) == "" {
		return nil, errors.New("provider create response must contain id")
	}
	if st.Outputs == nil {
		st.Outputs = make(map[string]interface{})
	}
	metadata, _ := st.Outputs["_resources"].(map[string]interface{})
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata[name] = map[string]interface{}{"type": resource.Type, "provider": resource.ProviderName, "endpoint": resource.ProviderURL}
	st.Outputs["_resources"] = metadata
	return result, nil
}

func (d *OrionDriver) client(ctx context.Context, endpoint string) (*plugin.Client, error) {
	if client := d.clients[endpoint]; client != nil {
		return client, nil
	}
	client, err := plugin.NewClient(ctx, plugin.ClientConfig{Endpoint: endpoint, Timeout: 30 * time.Second, Retry: &plugin.RetryConfig{MaxAttempts: 3, InitialDelay: 100 * time.Millisecond, MaxDelay: time.Second}})
	if err != nil {
		return nil, err
	}
	d.clients[endpoint] = client
	return client, nil
}

func providerEndpoint(resourceType, provider, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	key := strings.ToUpper(strings.NewReplacer("/", "_", ".", "_", ":", "_", "-", "_").Replace(resourceType))
	if value := strings.TrimSpace(os.Getenv("ORION_HEAT_PROVIDER_ENDPOINT_" + key)); value != "" {
		return value
	}
	if strings.TrimSpace(provider) != "" {
		providerKey := strings.ToUpper(strings.NewReplacer("/", "_", ".", "_", ":", "_", "-", "_").Replace(provider))
		if value := strings.TrimSpace(os.Getenv("ORION_HEAT_PROVIDER_ENDPOINT_" + providerKey)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(os.Getenv("ORION_HEAT_PROVIDER_ENDPOINT"))
}

func firstValue(values map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func mapValue(value interface{}) map[string]interface{} {
	if result, ok := value.(map[string]interface{}); ok {
		return result
	}
	return map[string]interface{}{}
}

func stringList(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func resourceState(st *stack.Stack, name string) map[string]interface{} {
	if st == nil || st.Outputs == nil {
		return nil
	}
	state, _ := st.Outputs[name].(map[string]interface{})
	return state
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}

func stringOr(value interface{}, fallback string) string {
	if text := stringValue(value); text != "" {
		return text
	}
	return fallback
}
