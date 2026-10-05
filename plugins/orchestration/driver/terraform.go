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

type TerraformDriver struct {
	workDir string
	mu      sync.Mutex
}

func NewTerraformDriver(cfg Config) (Driver, error) {
	if cfg.WorkDir == "" {
		cfg.WorkDir = "/tmp/orion-stacks"
	}

	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work dir: %w", err)
	}

	return &TerraformDriver{workDir: cfg.WorkDir}, nil
}

func (d *TerraformDriver) Create(ctx context.Context, st *stack.Stack, template *stack.Template) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return fmt.Errorf("failed to create stack dir: %w", err)
	}

	mainFile := filepath.Join(stackDir, "main.tf")
	if err := os.WriteFile(mainFile, []byte(template.Content), 0644); err != nil {
		return fmt.Errorf("failed to write main.tf: %w", err)
	}

	varsFile := filepath.Join(stackDir, "terraform.tfvars")
	if err := d.writeVarsFile(varsFile, st.Variables); err != nil {
		return fmt.Errorf("failed to write vars: %w", err)
	}

	if err := d.runTerraform(ctx, stackDir, "init"); err != nil {
		return fmt.Errorf("terraform init failed: %w", err)
	}

	if err := d.runTerraform(ctx, stackDir, "apply", "-auto-approve", "-input=false"); err != nil {
		st.State = stack.StackStateFailed
		return fmt.Errorf("terraform apply failed: %w", err)
	}

	st.State = stack.StackStateCompleted
	return nil
}

func (d *TerraformDriver) Update(ctx context.Context, st *stack.Stack, template *stack.Template) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)

	if template != nil && template.Content != "" {
		mainFile := filepath.Join(stackDir, "main.tf")
		if err := os.WriteFile(mainFile, []byte(template.Content), 0644); err != nil {
			return fmt.Errorf("failed to write main.tf: %w", err)
		}
	}

	if len(st.Variables) > 0 {
		varsFile := filepath.Join(stackDir, "terraform.tfvars")
		if err := d.writeVarsFile(varsFile, st.Variables); err != nil {
			return fmt.Errorf("failed to write vars: %w", err)
		}
	}

	if err := d.runTerraform(ctx, stackDir, "apply", "-auto-approve", "-input=false"); err != nil {
		st.State = stack.StackStateFailed
		return fmt.Errorf("terraform apply failed: %w", err)
	}

	st.State = stack.StackStateCompleted
	return nil
}

func (d *TerraformDriver) Delete(ctx context.Context, st *stack.Stack) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)

	if _, err := os.Stat(stackDir); os.IsNotExist(err) {
		return nil
	}

	if err := d.runTerraform(ctx, stackDir, "destroy", "-auto-approve", "-input=false"); err != nil {
		st.State = stack.StackStateFailed
		return fmt.Errorf("terraform destroy failed: %w", err)
	}

	os.RemoveAll(stackDir)
	st.State = stack.StackStateCompleted
	return nil
}

func (d *TerraformDriver) Suspend(ctx context.Context, st *stack.Stack) error {
	return fmt.Errorf("suspend is not supported by Terraform backend")
}

func (d *TerraformDriver) Resume(ctx context.Context, st *stack.Stack) error {
	return fmt.Errorf("resume is not supported by Terraform backend")
}

func (d *TerraformDriver) Check(ctx context.Context, st *stack.Stack) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)

	if err := d.runTerraform(ctx, stackDir, "validate"); err != nil {
		return fmt.Errorf("terraform validate failed: %w", err)
	}

	if err := d.runTerraform(ctx, stackDir, "plan", "-input=false", "-out=tfplan"); err != nil {
		return fmt.Errorf("terraform plan failed: %w", err)
	}

	return nil
}

func (d *TerraformDriver) GetOutputs(ctx context.Context, st *stack.Stack) (map[string]interface{}, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	stackDir := d.stackDir(st.ID)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "terraform", "output", "-json")
	cmd.Dir = stackDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("terraform output failed: %w, stderr: %s", err, stderr.String())
	}

	var outputs map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &outputs); err != nil {
		return nil, fmt.Errorf("failed to parse terraform output: %w", err)
	}

	result := make(map[string]interface{})
	for k, v := range outputs {
		if m, ok := v.(map[string]interface{}); ok {
			result[k] = m["value"]
		}
	}

	return result, nil
}

func (d *TerraformDriver) Close() error {
	return nil
}

func (d *TerraformDriver) stackDir(stackID string) string {
	return filepath.Join(d.workDir, stackID)
}

func (d *TerraformDriver) writeVarsFile(path string, vars map[string]interface{}) error {
	var lines []string
	for k, v := range vars {
		lines = append(lines, fmt.Sprintf("%s = %s", k, formatVarValue(v)))
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func (d *TerraformDriver) runTerraform(ctx context.Context, dir string, args ...string) error {
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmd.Env = append(os.Environ(),
		"TF_LOG=ERROR",
		"TF_INPUT=0",
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terraform %s failed: %w\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), err, stdout.String(), stderr.String())
	}

	return nil
}

func formatVarValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		if strings.Contains(val, " ") || strings.HasPrefix(val, "{") || strings.HasPrefix(val, "[") {
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
			items = append(items, formatVarValue(item))
		}
		return fmt.Sprintf("[%s]", strings.Join(items, ", "))
	case map[string]interface{}:
		var items []string
		for k, v := range val {
			items = append(items, fmt.Sprintf("%s = %s", k, formatVarValue(v)))
		}
		return fmt.Sprintf("{%s}", strings.Join(items, ", "))
	default:
		return fmt.Sprintf(`"%v"`, v)
	}
}
