package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/horizon/orion/plugins/orchestration/driver"
	"github.com/horizon/orion/plugins/orchestration/stack"
	"github.com/horizon/orion/sdk/go/plugin"
	"gopkg.in/yaml.v3"
)

var (
	_stacks         sync.Map
	_defaultBackend = os.Getenv("ORION_ORCHESTRATION_BACKEND")
	_stateFile      = os.Getenv("ORION_ORCHESTRATION_STATE_FILE")
)

type persistedStack struct {
	Stack            *stack.Stack    `json:"stack"`
	Backend          string          `json:"backend"`
	Template         *stack.Template `json:"template,omitempty"`
	PreviousTemplate *stack.Template `json:"previous_template,omitempty"`
	Events           []stack.Event   `json:"events,omitempty"`
}

func main() {
	if _defaultBackend == "" {
		_defaultBackend = "terraform"
	}
	if err := loadStacks(); err != nil {
		log.Fatalf("failed to load orchestration state: %v", err)
	}

	p := plugin.New(plugin.Config{ID: "orchestration-plugin", Name: "Orion Orchestration", Version: "1.0.0", Vendor: "Orion"})
	p.Resource("orion.io/orchestration.stack", "v1").
		Handle("create", handleCreate).
		Handle("get", handleGet).
		Handle("delete", handleDelete).
		Handle("update", handleUpdate).
		Handle("suspend", handleSuspend).
		Handle("resume", handleResume).
		Handle("check", handleCheck).
		Handle("list", handleList).
		Handle("outputs", handleOutputs).
		Handle("preview", handlePreview).
		Handle("rollback", handleRollback).
		Handle("events", handleEvents).
		Handle("validate_template", handleValidateTemplate).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50069"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := p.Serve(ctx, endpoint); err != nil {
		log.Fatalf("orchestration plugin stopped: %v", err)
	}
}

func handleCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input stack.CreateInput
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	if input.Name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	backend := input.Backend
	if backend == "" {
		backend = _defaultBackend
	}

	st := &stack.Stack{
		ID:        uuid.New().String(),
		Name:      input.Name,
		TenantID:  input.TenantID,
		ProjectID: input.ProjectID,
		Kind:      "orion.io/orchestration.stack",
		State:     stack.StackStatePending,
		Variables: input.Variables,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	template := &stack.Template{
		Format:  input.TemplateFormat,
		Content: input.TemplateContent,
	}
	if err := validateDeclarativeTemplate(template.Format, template.Content); err != nil {
		return errorResponse(err), nil
	}

	drv, err := driver.NewDriver(backend, driver.Config{
		Backend: backend,
		WorkDir: os.Getenv("ORION_ORCHESTRATION_WORKDIR"),
	})
	if err != nil {
		return errorResponse(fmt.Errorf("failed to create driver: %w", err)), nil
	}

	st.State = stack.StackStateRunning
	swd := &stackWithDriver{st: st, drv: drv, template: template, backend: backend}
	swd.addEvent("create", "RUNNING", "stack creation started")
	_stacks.Store(st.ID, swd)
	if err := persistStacks(); err != nil {
		return errorResponse(err), nil
	}

	if err := drv.Create(ctx, st, template); err != nil {
		st.State = stack.StackStateFailed
		st.Error = err.Error()
		swd.addEvent("create", "FAILED", err.Error())
		return errorResponse(err), nil
	}

	now := time.Now()
	st.CompletedAt = &now
	st.UpdatedAt = time.Now()
	swd.addEvent("create", "COMPLETED", "stack creation completed")
	if err := persistStacks(); err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(st),
	}, nil
}

func handleGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	swd, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(swd.(*stackWithDriver).st),
	}, nil
}

func handleDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	swd, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}

	s := swd.(*stackWithDriver)
	s.st.State = stack.StackStateRunning

	if err := s.drv.Delete(ctx, s.st); err != nil {
		s.st.State = stack.StackStateFailed
		s.st.Error = err.Error()
		return errorResponse(err), nil
	}
	_stacks.Delete(id)
	if err := persistStacks(); err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(s.st),
	}, nil
}

func handleUpdate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input stack.UpdateInput
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	if input.ID == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	swd, ok := _stacks.Load(input.ID)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", input.ID)), nil
	}

	s := swd.(*stackWithDriver)
	s.mu.Lock()
	previousTemplate := s.template
	currentTemplate := s.template
	s.mu.Unlock()
	s.st.Variables = input.Variables
	s.st.State = stack.StackStateRunning
	s.st.UpdatedAt = time.Now()

	var template *stack.Template
	if input.TemplateContent != "" {
		template = &stack.Template{Content: input.TemplateContent}
	} else {
		template = currentTemplate
	}

	if err := s.drv.Update(ctx, s.st, template); err != nil {
		s.st.State = stack.StackStateFailed
		s.st.Error = err.Error()
		s.addEvent("update", "FAILED", err.Error())
		return errorResponse(err), nil
	}
	if template != nil && template.Content != "" {
		s.mu.Lock()
		s.previousTemplate = previousTemplate
		s.template = template
		s.mu.Unlock()
	}

	now := time.Now()
	s.st.CompletedAt = &now
	s.addEvent("update", "COMPLETED", "stack update completed")
	if err := persistStacks(); err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(s.st),
	}, nil
}

func handleSuspend(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	swd, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}

	s := swd.(*stackWithDriver)
	s.st.State = stack.StackStateRunning

	if err := s.drv.Suspend(ctx, s.st); err != nil {
		s.st.State = stack.StackStateFailed
		s.st.Error = err.Error()
		return errorResponse(err), nil
	}

	s.st.State = stack.StackStateSuspended
	if err := persistStacks(); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(s.st),
	}, nil
}

func handleResume(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	swd, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}

	s := swd.(*stackWithDriver)
	s.st.State = stack.StackStateRunning

	if err := s.drv.Resume(ctx, s.st); err != nil {
		s.st.State = stack.StackStateFailed
		s.st.Error = err.Error()
		return errorResponse(err), nil
	}

	s.st.State = stack.StackStateCompleted
	if err := persistStacks(); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(s.st),
	}, nil
}

func handleCheck(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	swd, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}

	s := swd.(*stackWithDriver)

	if err := s.drv.Check(ctx, s.st); err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"id": id, "status": "valid"}),
	}, nil
}

func handleList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if req.Payload != nil {
		json.Unmarshal(req.Payload, &input)
	}

	var items []map[string]interface{}
	filter := getString(input, "tenantId")

	_stacks.Range(func(key, value interface{}) bool {
		s := value.(*stackWithDriver).st
		if filter == "" || s.TenantID == filter {
			items = append(items, map[string]interface{}{
				"id":        s.ID,
				"name":      s.Name,
				"state":     s.State,
				"createdAt": s.CreatedAt,
				"updatedAt": s.UpdatedAt,
			})
		}
		return true
	})

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(map[string]interface{}{"items": items, "total": len(items)}),
	}, nil
}

func handlePreview(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}
	value, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}
	swd := value.(*stackWithDriver)
	swd.mu.Lock()
	template := swd.template
	backend := swd.backend
	swd.mu.Unlock()
	if backend == "orion" {
		if err := driver.ValidateNativeTemplate(template); err != nil {
			return errorResponse(err), nil
		}
	}
	if err := swd.drv.Check(ctx, swd.st); err != nil {
		return errorResponse(err), nil
	}
	swd.addEvent("preview", "COMPLETED", "plan/validation completed")
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]interface{}{"id": id, "status": "valid"})}, nil
}

func handleValidateTemplate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input struct {
		Format  string `json:"format"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	if err := validateDeclarativeTemplate(input.Format, input.Content); err != nil {
		return errorResponse(err), nil
	}
	if input.Format == "heat" || input.Format == "yaml" || input.Format == "yml" {
		if err := driver.ValidateNativeTemplate(&stack.Template{Format: input.Format, Content: input.Content}); err != nil {
			return errorResponse(err), nil
		}
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]string{"status": "valid"})}, nil
}

func validateDeclarativeTemplate(format, content string) error {
	if content == "" || (format != "heat" && format != "yaml" && format != "yml") {
		return nil
	}
	var document map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return fmt.Errorf("invalid declarative template: %w", err)
	}
	resourcesValue, ok := document["resources"]
	if !ok {
		resourcesValue = document["Resources"]
	}
	resources, ok := resourcesValue.(map[string]interface{})
	if !ok {
		return fmt.Errorf("template resources must be an object")
	}
	dependencies := make(map[string][]string, len(resources))
	for name, raw := range resources {
		definition, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("resource %s must be an object", name)
		}
		if typ, _ := definition["type"].(string); typ == "" {
			if typ, _ = definition["Type"].(string); typ == "" {
				return fmt.Errorf("resource %s has no type", name)
			}
		}
		value := definition["depends_on"]
		if value == nil {
			value = definition["DependsOn"]
		}
		switch dependency := value.(type) {
		case string:
			dependencies[name] = []string{dependency}
		case []interface{}:
			for _, item := range dependency {
				dep, ok := item.(string)
				if !ok {
					return fmt.Errorf("resource %s has an invalid dependency", name)
				}
				dependencies[name] = append(dependencies[name], dep)
			}
		}
		for _, dep := range dependencies[name] {
			if _, exists := resources[dep]; !exists {
				return fmt.Errorf("resource %s depends on unknown resource %s", name, dep)
			}
		}
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf("resource dependency cycle at %s", name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		for _, dep := range dependencies[name] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[name] = 2
		return nil
	}
	for name := range resources {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func handleRollback(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}
	value, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}
	swd := value.(*stackWithDriver)
	swd.mu.Lock()
	previous := swd.previousTemplate
	swd.mu.Unlock()
	if previous == nil {
		return errorResponse(fmt.Errorf("stack has no previous template to restore")), nil
	}
	swd.st.State = stack.StackStateRunning
	swd.addEvent("rollback", "RUNNING", "restoring previous template")
	if err := swd.drv.Update(ctx, swd.st, previous); err != nil {
		swd.st.State = stack.StackStateFailed
		swd.st.Error = err.Error()
		swd.addEvent("rollback", "FAILED", err.Error())
		return errorResponse(err), nil
	}
	swd.st.State = stack.StackStateCompleted
	swd.st.UpdatedAt = time.Now()
	swd.addEvent("rollback", "COMPLETED", "previous template restored")
	swd.mu.Lock()
	current := swd.template
	swd.template = previous
	swd.previousTemplate = current
	swd.mu.Unlock()
	if err := persistStacks(); err != nil {
		return errorResponse(err), nil
	}
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(swd.st)}, nil
}

func handleEvents(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}
	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}
	value, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}
	swd := value.(*stackWithDriver)
	swd.mu.Lock()
	events := append([]stack.Event(nil), swd.events...)
	swd.mu.Unlock()
	return &plugin.ResourceResponse{Success: true, Result: mustMarshal(map[string]interface{}{"items": events, "total": len(events)})}, nil
}

func handleOutputs(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	id := getString(input, "id")
	if id == "" {
		return errorResponse(fmt.Errorf("id is required")), nil
	}

	swd, ok := _stacks.Load(id)
	if !ok {
		return errorResponse(fmt.Errorf("stack not found: %s", id)), nil
	}

	s := swd.(*stackWithDriver)

	outputs, err := s.drv.GetOutputs(ctx, s.st)
	if err != nil {
		return errorResponse(err), nil
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  mustMarshal(outputs),
	}, nil
}

type stackWithDriver struct {
	mu               sync.Mutex
	st               *stack.Stack
	drv              driver.Driver
	template         *stack.Template
	previousTemplate *stack.Template
	events           []stack.Event
	backend          string
}

func (s *stackWithDriver) addEvent(action, status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, stack.Event{ID: uuid.New().String(), StackID: s.st.ID, Action: action, Status: status, Message: message, Timestamp: time.Now().UTC()})
}

func loadStacks() error {
	if strings.TrimSpace(_stateFile) == "" {
		return nil
	}
	payload, err := os.ReadFile(_stateFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved []persistedStack
	if err := json.Unmarshal(payload, &saved); err != nil {
		return fmt.Errorf("decode orchestration state: %w", err)
	}
	for _, item := range saved {
		if item.Stack == nil || item.Stack.ID == "" {
			continue
		}
		backend := item.Backend
		if backend == "" {
			backend = _defaultBackend
		}
		drv, err := driver.NewDriver(backend, driver.Config{Backend: backend, WorkDir: os.Getenv("ORION_ORCHESTRATION_WORKDIR")})
		if err != nil {
			return fmt.Errorf("restore stack %s: %w", item.Stack.ID, err)
		}
		_stacks.Store(item.Stack.ID, &stackWithDriver{st: item.Stack, drv: drv, template: item.Template, previousTemplate: item.PreviousTemplate, events: item.Events, backend: backend})
	}
	return nil
}

func persistStacks() error {
	if strings.TrimSpace(_stateFile) == "" {
		return nil
	}
	saved := make([]persistedStack, 0)
	_stacks.Range(func(_, value interface{}) bool {
		swd := value.(*stackWithDriver)
		swd.mu.Lock()
		item := persistedStack{Stack: cloneStack(swd.st), Backend: swd.backend, Template: swd.template, PreviousTemplate: swd.previousTemplate, Events: append([]stack.Event(nil), swd.events...)}
		swd.mu.Unlock()
		saved = append(saved, item)
		return true
	})
	payload, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(_stateFile)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(directory, ".orion-heat-state-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, _stateFile)
}

func cloneStack(item *stack.Stack) *stack.Stack {
	if item == nil {
		return nil
	}
	copy := *item
	if item.Variables != nil {
		copy.Variables = make(map[string]interface{}, len(item.Variables))
		for key, value := range item.Variables {
			copy.Variables[key] = value
		}
	}
	if item.Outputs != nil {
		copy.Outputs = make(map[string]interface{}, len(item.Outputs))
		for key, value := range item.Outputs {
			copy.Outputs[key] = value
		}
	}
	return &copy
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
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
