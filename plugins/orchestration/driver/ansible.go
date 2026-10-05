package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/horizon/orion/plugins/orchestration/stack"
)

type AnsibleDriver struct {
	workDir string
	mu      sync.Mutex
}

func NewAnsibleDriver(cfg Config) (Driver, error) {
	if cfg.WorkDir == "" {
		cfg.WorkDir = "/tmp/orion-playbooks"
	}

	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work dir: %w", err)
	}

	return &AnsibleDriver{workDir: cfg.WorkDir}, nil
}

func (d *AnsibleDriver) Create(ctx context.Context, st *stack.Stack, template *stack.Template) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)
	if err := os.MkdirAll(filepath.Join(stackDir, "roles"), 0755); err != nil {
		return fmt.Errorf("failed to create stack dir: %w", err)
	}

	playbookFile := filepath.Join(stackDir, "playbook.yml")
	if err := os.WriteFile(playbookFile, []byte(template.Content), 0644); err != nil {
		return fmt.Errorf("failed to write playbook: %w", err)
	}

	if err := d.writeInventory(ctx, stackDir, st.Variables); err != nil {
		return fmt.Errorf("failed to write inventory: %w", err)
	}

	if err := d.writeVars(ctx, stackDir, st.Variables); err != nil {
		return fmt.Errorf("failed to write vars: %w", err)
	}

	if err := d.runAnsible(ctx, stackDir, "playbook.yml", "create"); err != nil {
		st.State = stack.StackStateFailed
		return fmt.Errorf("ansible create failed: %w", err)
	}

	st.State = stack.StackStateCompleted
	return nil
}

func (d *AnsibleDriver) Update(ctx context.Context, st *stack.Stack, template *stack.Template) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)

	if template != nil && template.Content != "" {
		playbookFile := filepath.Join(stackDir, "playbook.yml")
		if err := os.WriteFile(playbookFile, []byte(template.Content), 0644); err != nil {
			return fmt.Errorf("failed to write playbook: %w", err)
		}
	}

	if err := d.writeVars(ctx, stackDir, st.Variables); err != nil {
		return fmt.Errorf("failed to write vars: %w", err)
	}

	if err := d.runAnsible(ctx, stackDir, "playbook.yml", "update"); err != nil {
		st.State = stack.StackStateFailed
		return fmt.Errorf("ansible update failed: %w", err)
	}

	st.State = stack.StackStateCompleted
	return nil
}

func (d *AnsibleDriver) Delete(ctx context.Context, st *stack.Stack) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)

	if _, err := os.Stat(stackDir); os.IsNotExist(err) {
		return nil
	}

	if err := d.runAnsible(ctx, stackDir, "playbook.yml", "delete"); err != nil {
		st.State = stack.StackStateFailed
		return fmt.Errorf("ansible delete failed: %w", err)
	}

	os.RemoveAll(stackDir)
	st.State = stack.StackStateCompleted
	return nil
}

func (d *AnsibleDriver) Suspend(ctx context.Context, st *stack.Stack) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)
	return d.runAnsible(ctx, stackDir, "playbook.yml", "suspend")
}

func (d *AnsibleDriver) Resume(ctx context.Context, st *stack.Stack) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)
	return d.runAnsible(ctx, stackDir, "playbook.yml", "resume")
}

func (d *AnsibleDriver) Check(ctx context.Context, st *stack.Stack) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)

	playbookFile := filepath.Join(stackDir, "playbook.yml")
	if err := d.runAnsibleSyntaxCheck(ctx, stackDir, playbookFile); err != nil {
		return fmt.Errorf("ansible syntax check failed: %w", err)
	}

	return nil
}

func (d *AnsibleDriver) GetOutputs(ctx context.Context, st *stack.Stack) (map[string]interface{}, error) {
	outputsFile := filepath.Join(d.stackDir(st.ID), "output.json")

	data, err := os.ReadFile(outputsFile)
	if err != nil {
		return nil, fmt.Errorf("outputs file not found: %w", err)
	}

	var outputs map[string]interface{}
	if err := json.Unmarshal(data, &outputs); err != nil {
		return nil, fmt.Errorf("failed to parse outputs: %w", err)
	}

	return outputs, nil
}

func (d *AnsibleDriver) Close() error {
	return nil
}

func (d *AnsibleDriver) stackDir(stackID string) string {
	return filepath.Join(d.workDir, stackID)
}

func (d *AnsibleDriver) writeInventory(ctx context.Context, dir string, vars map[string]interface{}) error {
	inventory := `[all]
localhost ansible_connection=local
`
	return os.WriteFile(filepath.Join(dir, "inventory"), []byte(inventory), 0644)
}

func (d *AnsibleDriver) writeVars(ctx context.Context, dir string, vars map[string]interface{}) error {
	lines := []string{"---"}

	for k, v := range vars {
		if sensitive, ok := vars[k+"_sensitive"].(bool); ok && sensitive {
			lines = append(lines, fmt.Sprintf("%s: \"{{ %s }}\"", k, k))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", k, formatAnsibleVar(v)))
	}

	groupVarsDir := filepath.Join(dir, "group_vars", "all")
	if err := os.MkdirAll(groupVarsDir, 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(groupVarsDir, "main.yml"), []byte(strings.Join(lines, "\n")), 0644)
}

func (d *AnsibleDriver) runAnsible(ctx context.Context, dir, playbook, tag string) error {
	args := []string{
		"ansible-playbook",
		filepath.Join(dir, playbook),
		"-i", filepath.Join(dir, "inventory"),
		"--tags", tag,
		"-e", fmt.Sprintf("stack_id=%s", filepath.Base(dir)),
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ansible-playbook failed: %w\nstdout: %s\nstderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

func (d *AnsibleDriver) runAnsibleSyntaxCheck(ctx context.Context, dir, playbook string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ansible-playbook", playbook, "--syntax-check")
	cmd.Dir = dir
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("syntax check failed: %w, stderr: %s", err, stderr.String())
	}

	return nil
}

func formatAnsibleVar(v interface{}) string {
	switch val := v.(type) {
	case string:
		if strings.Contains(val, " ") || strings.HasPrefix(val, "{{") {
			return fmt.Sprintf(`"%s"`, val)
		}
		return val
	case bool:
		return fmt.Sprintf("%t", val)
	case int, int64, float64:
		return fmt.Sprintf("%v", val)
	case []interface{}:
		var items []string
		for _, item := range val {
			items = append(items, formatAnsibleVar(item))
		}
		return fmt.Sprintf("[%s]", strings.Join(items, ", "))
	case map[string]interface{}:
		var items []string
		for k, v := range val {
			items = append(items, fmt.Sprintf("%s: %s", k, formatAnsibleVar(v)))
		}
		return fmt.Sprintf("{%s}", strings.Join(items, ", "))
	default:
		return fmt.Sprintf(`"%v"`, val)
	}
}
