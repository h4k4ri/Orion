package driver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/horizon/orion/plugins/containers/container"
)

type ContainerdDriver struct {
	address string
}

func NewContainerdDriver(cfg Config) (Driver, error) {
	addr := cfg.Address
	if addr == "" {
		addr = "/run/containerd/containerd.sock"
	}
	return &ContainerdDriver{address: addr}, nil
}

func (d *ContainerdDriver) ListContainers(ctx context.Context, opts ListOptions) ([]container.Container, error) {
	args := []string{"ctr", "-a", d.address, "containers", "list"}
	if opts.All {
		args = append(args, "-a")
	}

	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ctr containers list failed: %w, output: %s", err, output)
	}

	return parseCtrContainers(string(output)), nil
}

func (d *ContainerdDriver) GetContainer(ctx context.Context, id string) (*container.Container, error) {
	args := []string{"ctr", "-a", d.address, "containers", "info", id}
	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ctr containers info failed: %w", err)
	}

	var info struct {
		ID      string `json:"ID"`
		Name    string `json:"Name"`
		Image   string `json:"Image"`
		Runtime struct {
			Name string `json:"name"`
		} `json:"Runtime"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse container info: %w", err)
	}

	return &container.Container{
		ID:        info.ID,
		Name:      info.Name,
		Image:     info.Image,
		Labels:    info.Labels,
		State:     container.ContainerStateUnknown,
		CreatedAt: time.Now(),
	}, nil
}

func (d *ContainerdDriver) CreateContainer(ctx context.Context, input *container.CreateContainerInput) (*container.Container, error) {
	name := input.Name
	if name == "" {
		name = "container-" + randomID()
	}

	args := []string{
		"ctr", "-a", d.address, "containers", "create",
		"--label", "app=" + name,
	}

	for _, mount := range input.Mounts {
		args = append(args, "-m", fmt.Sprintf("%s:%s", mount.Source, mount.Destination))
		if mount.ReadOnly {
			args = append(args, "--readonly")
		}
	}

	args = append(args, input.Image, name)

	for _, env := range input.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", env.Name, env.Value))
	}

	for _, port := range input.Ports {
		args = append(args, "-p", fmt.Sprintf("%d:%d/%s", port.HostPort, port.ContainerPort, port.Protocol))
	}

	if _, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ctr container create failed: %w", err)
	}

	return &container.Container{
		ID:        name,
		Name:      name,
		Image:     input.Image,
		Command:   input.Command,
		Args:      input.Args,
		Env:       input.Env,
		Ports:     input.Ports,
		Mounts:    input.Mounts,
		Labels:    input.Labels,
		State:     container.ContainerStateCreated,
		CreatedAt: time.Now(),
	}, nil
}

func (d *ContainerdDriver) StartContainer(ctx context.Context, id string) error {
	args := []string{"ctr", "-a", d.address, "tasks", "start", "-d", id}
	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ctr task start failed: %w, output: %s", err, output)
	}
	return nil
}

func (d *ContainerdDriver) StopContainer(ctx context.Context, id string, timeout int) error {
	args := []string{"ctr", "-a", d.address, "tasks", "kill", id}
	if timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", timeout))
	}
	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ctr task kill failed: %w, output: %s", err, output)
	}
	return nil
}

func (d *ContainerdDriver) DeleteContainer(ctx context.Context, id string) error {
	args := []string{"ctr", "-a", d.address, "containers", "rm", id}
	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ctr container rm failed: %w, output: %s", err, output)
	}
	return nil
}

func (d *ContainerdDriver) LogsContainer(ctx context.Context, id string, opts LogsOptions) (string, error) {
	args := []string{"ctr", "-a", d.address, "tasks", "logs", id}
	if opts.Tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", opts.Tail))
	}
	if opts.Since != "" {
		args = append(args, "--since", opts.Since)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ctr logs failed: %w", err)
	}

	var logs string
	if opts.Stdout {
		logs += stdout.String()
	}
	if opts.Stderr {
		logs += stderr.String()
	}
	return logs, nil
}

func (d *ContainerdDriver) ExecContainer(ctx context.Context, id string, cmd []string) (string, error) {
	args := append([]string{"ctr", "-a", d.address, "tasks", "exec", "--id", id, "--"}, cmd...)
	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ctr exec failed: %w, output: %s", err, output)
	}
	return string(output), nil
}

func (d *ContainerdDriver) ListImages(ctx context.Context, opts ListOptions) ([]container.Image, error) {
	args := []string{"ctr", "-a", d.address, "images", "list"}
	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ctr images list failed: %w, output: %s", err, output)
	}

	return parseCtrImages(string(output)), nil
}

func (d *ContainerdDriver) PullImage(ctx context.Context, ref string) error {
	args := []string{"ctr", "-a", d.address, "images", "pull", ref}
	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ctr image pull failed: %w, output: %s", err, output)
	}
	return nil
}

func (d *ContainerdDriver) DeleteImage(ctx context.Context, ref string) error {
	args := []string{"ctr", "-a", d.address, "images", "rm", ref}
	output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ctr image rm failed: %w, output: %s", err, output)
	}
	return nil
}

func (d *ContainerdDriver) Close() error {
	return nil
}

func parseCtrContainers(output string) []container.Container {
	var containers []container.Container
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "NAME") || line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			containers = append(containers, container.Container{
				ID:    parts[0],
				Name:  parts[0],
				Image: parts[1],
				State: container.ContainerStateUnknown,
			})
		}
	}
	return containers
}

func parseCtrImages(output string) []container.Image {
	var images []container.Image
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "REF") || line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			repoTag := strings.Split(parts[0], ":")
			images = append(images, container.Image{
				ID:         parts[0],
				Repository: repoTag[0],
				Tag:        "latest",
			})
		}
	}
	return images
}

func randomID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
