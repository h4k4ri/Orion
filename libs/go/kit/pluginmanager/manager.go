// Package pluginmanager provides a small local process manager for Orion
// plugins. Plugins remain ordinary executables; the manager only supplies
// lifecycle, endpoint allocation, manifest discovery, and diagnostics.
package pluginmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const startupTimeout = 15 * time.Second

type Spec struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Vendor   string            `json:"vendor,omitempty"`
	Binary   string            `json:"binary"`
	Args     []string          `json:"args,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Endpoint string            `json:"endpoint,omitempty"`
	Manifest string            `json:"manifest,omitempty"`
}

type State struct {
	Spec      Spec      `json:"spec"`
	Status    string    `json:"status"`
	PID       int       `json:"pid,omitempty"`
	Endpoint  string    `json:"endpoint,omitempty"`
	Manifest  string    `json:"manifest,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
}

type Diagnostic struct {
	ID             string `json:"id"`
	Installed      bool   `json:"installed"`
	Binary         string `json:"binary"`
	BinaryExists   bool   `json:"binary_exists"`
	Status         string `json:"status"`
	Endpoint       string `json:"endpoint,omitempty"`
	Manifest       string `json:"manifest,omitempty"`
	ManifestExists bool   `json:"manifest_exists"`
	Reachable      bool   `json:"reachable"`
	Error          string `json:"error,omitempty"`
}

type persisted struct {
	Specs  map[string]Spec  `json:"specs"`
	States map[string]State `json:"states,omitempty"`
}

type processHandle struct {
	cmd  *exec.Cmd
	done chan error
}

type Manager struct {
	mu        sync.RWMutex
	statePath string
	specs     map[string]Spec
	states    map[string]State
	processes map[string]*processHandle
}

func New(statePath string) (*Manager, error) {
	if statePath == "" {
		return nil, errors.New("plugin manager state path is required")
	}
	m := &Manager{
		statePath: statePath,
		specs:     make(map[string]Spec),
		states:    make(map[string]State),
		processes: make(map[string]*processHandle),
	}
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read plugin manager state: %w", err)
	}
	var saved persisted
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("decode plugin manager state: %w", err)
	}
	for id, spec := range saved.Specs {
		m.specs[id] = spec
		state := saved.States[id]
		state.Spec = spec
		if state.PID > 0 && processMatches(state.PID, spec.Binary) {
			state.Status = "running"
		} else {
			state.Status = "stopped"
			state.PID = 0
			state.Endpoint = ""
			state.Manifest = ""
		}
		m.states[id] = state
	}
	return m, nil
}

func (m *Manager) Install(spec Spec) error {
	if err := validateSpec(spec); err != nil {
		return err
	}
	abs, err := filepath.Abs(spec.Binary)
	if err != nil {
		return fmt.Errorf("resolve plugin binary: %w", err)
	}
	spec.Binary = abs
	if spec.Manifest != "" {
		if manifest, err := filepath.Abs(spec.Manifest); err == nil {
			spec.Manifest = manifest
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, running := m.processes[spec.ID]; running && current.cmd.Process != nil {
		return fmt.Errorf("plugin %q is running", spec.ID)
	}
	m.specs[spec.ID] = spec
	m.states[spec.ID] = State{Spec: spec, Status: "stopped", Manifest: spec.Manifest}
	return m.persistLocked()
}

func (m *Manager) Uninstall(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.specs[id]; !ok {
		return fmt.Errorf("plugin %q is not installed", id)
	}
	if _, running := m.processes[id]; running || (m.states[id].PID > 0 && processMatches(m.states[id].PID, m.states[id].Spec.Binary)) {
		return fmt.Errorf("plugin %q is running; stop it first", id)
	}
	delete(m.specs, id)
	delete(m.states, id)
	return m.persistLocked()
}

func (m *Manager) List() []State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]State, 0, len(m.specs))
	for id, spec := range m.specs {
		state := m.states[id]
		state.Spec = spec
		result = append(result, state)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Spec.ID < result[j].Spec.ID })
	return result
}

func (m *Manager) Start(ctx context.Context, id string) error {
	m.mu.Lock()
	spec, ok := m.specs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin %q is not installed", id)
	}
	if _, running := m.processes[id]; running {
		m.mu.Unlock()
		return fmt.Errorf("plugin %q is already running", id)
	}
	if state := m.states[id]; state.PID > 0 && processMatches(state.PID, state.Spec.Binary) {
		m.mu.Unlock()
		return fmt.Errorf("plugin %q is already running with pid %d", id, state.PID)
	}
	endpoint := spec.Endpoint
	if endpoint == "" {
		var err error
		endpoint, err = reserveEndpoint()
		if err != nil {
			m.mu.Unlock()
			return err
		}
	}
	manifestPath := spec.Manifest
	if manifestPath == "" {
		manifestPath = filepath.Join(m.statePath+".d", id+".json")
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("create plugin manifest directory: %w", err)
	}
	_ = os.Remove(manifestPath)
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for key, value := range spec.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	// Lifecycle values are owned by the manager and cannot be overridden by a
	// descriptor environment entry.
	cmd.Env = append(cmd.Env, "ORION_PLUGIN_ENDPOINT="+endpoint, "ORION_PLUGIN_MANIFEST="+manifestPath)
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("start plugin %q: %w", id, err)
	}
	h := &processHandle{cmd: cmd, done: make(chan error, 1)}
	m.processes[id] = h
	m.states[id] = State{Spec: spec, Status: "starting", PID: cmd.Process.Pid, Endpoint: endpoint, Manifest: manifestPath, StartedAt: time.Now().UTC()}
	if err := m.persistLocked(); err != nil {
		_ = cmd.Process.Kill()
		delete(m.processes, id)
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()

	go m.waitProcess(id, h)
	startupCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	if err := waitReady(startupCtx, endpoint, manifestPath, h.done); err != nil {
		_ = m.Stop(context.Background(), id)
		return fmt.Errorf("plugin %q did not become ready: %w", id, err)
	}

	m.mu.Lock()
	if state, exists := m.states[id]; exists {
		state.Status = "running"
		state.LastError = ""
		m.states[id] = state
		_ = m.persistLocked()
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.RLock()
	h, ok := m.processes[id]
	state := m.states[id]
	m.mu.RUnlock()
	if !ok && state.PID == 0 {
		return fmt.Errorf("plugin %q is not running", id)
	}
	if !ok {
		if !processMatches(state.PID, state.Spec.Binary) {
			m.mu.Lock()
			state.Status = "stopped"
			state.PID = 0
			state.Endpoint = ""
			state.Manifest = ""
			m.states[id] = state
			err := m.persistLocked()
			m.mu.Unlock()
			if err != nil {
				return err
			}
			return fmt.Errorf("plugin %q is not running", id)
		}
		process, err := os.FindProcess(state.PID)
		if err != nil {
			return fmt.Errorf("find plugin %q process: %w", id, err)
		}
		if err := process.Signal(os.Interrupt); err != nil {
			_ = process.Kill()
		}
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for processAlive(state.PID) {
			select {
			case <-ctx.Done():
				_ = process.Kill()
				return ctx.Err()
			case <-deadline.C:
				_ = process.Kill()
				return fmt.Errorf("plugin %q did not stop", id)
			case <-ticker.C:
			}
		}
		m.mu.Lock()
		state = m.states[id]
		state.Status = "stopped"
		state.PID = 0
		state.Endpoint = ""
		state.Manifest = ""
		m.states[id] = state
		err = m.persistLocked()
		m.mu.Unlock()
		return err
	}
	if err := h.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = h.cmd.Process.Kill()
	}
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		_ = h.cmd.Process.Kill()
		return ctx.Err()
	}
}

func (m *Manager) Doctor(_ context.Context, id string) (Diagnostic, error) {
	m.mu.RLock()
	spec, installed := m.specs[id]
	state := m.states[id]
	m.mu.RUnlock()
	if !installed {
		return Diagnostic{ID: id, Installed: false, Error: "plugin is not installed"}, fmt.Errorf("plugin %q is not installed", id)
	}
	d := Diagnostic{ID: id, Installed: true, Binary: spec.Binary, Status: state.Status, Endpoint: state.Endpoint, Manifest: state.Manifest}
	_, err := os.Stat(spec.Binary)
	d.BinaryExists = err == nil
	if state.Manifest != "" {
		_, err := os.Stat(state.Manifest)
		d.ManifestExists = err == nil
	}
	if state.Endpoint != "" {
		conn, err := net.DialTimeout("tcp", state.Endpoint, time.Second)
		if err == nil {
			d.Reachable = true
			_ = conn.Close()
		}
	}
	if !d.BinaryExists {
		d.Error = "plugin binary does not exist"
	} else if state.Status == "running" && !d.Reachable {
		d.Error = "plugin endpoint is not reachable"
	}
	return d, nil
}

func Discover(dir string) ([]Spec, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read plugin directory: %w", err)
	}
	result := make([]Spec, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".plugin.json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read plugin descriptor %s: %w", path, err)
		}
		var spec Spec
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("decode plugin descriptor %s: %w", path, err)
		}
		if !filepath.IsAbs(spec.Binary) {
			spec.Binary = filepath.Join(dir, spec.Binary)
		}
		if err := validateSpec(spec); err != nil {
			return nil, fmt.Errorf("descriptor %s: %w", path, err)
		}
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func validateSpec(spec Spec) error {
	for field, value := range map[string]string{"id": spec.ID, "name": spec.Name, "version": spec.Version, "binary": spec.Binary} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("plugin %s is required", field)
		}
	}
	if filepath.Base(spec.Binary) == "." || filepath.Base(spec.Binary) == string(filepath.Separator) {
		return errors.New("plugin binary is invalid")
	}
	return nil
}

func reserveEndpoint() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("reserve plugin endpoint: %w", err)
	}
	endpoint := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("release endpoint reservation: %w", err)
	}
	return endpoint, nil
}

func waitReady(ctx context.Context, endpoint, manifestPath string, done <-chan error) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(manifestPath); err == nil {
			conn, err := net.DialTimeout("tcp", endpoint, 250*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
		select {
		case err := <-done:
			if err == nil {
				return errors.New("process exited before readiness")
			}
			return fmt.Errorf("process exited: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) waitProcess(id string, h *processHandle) {
	err := h.cmd.Wait()
	h.done <- err
	m.mu.Lock()
	delete(m.processes, id)
	state := m.states[id]
	state.PID = 0
	if err != nil {
		state.Status = "failed"
		state.LastError = err.Error()
	} else {
		state.Status = "stopped"
		state.LastError = ""
	}
	m.states[id] = state
	_ = m.persistLocked()
	m.mu.Unlock()
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func processMatches(pid int, binary string) bool {
	if !processAlive(pid) {
		return false
	}
	actual, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		// Keep the manager usable on platforms without /proc; processAlive still
		// provides the best available liveness check there.
		return true
	}
	want, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return false
	}
	actual, err = filepath.EvalSymlinks(actual)
	return err == nil && actual == want
}

func (m *Manager) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o755); err != nil {
		return fmt.Errorf("create plugin manager state directory: %w", err)
	}
	saved := persisted{Specs: m.specs, States: m.states}
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plugin manager state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.statePath), ".plugins-*.tmp")
	if err != nil {
		return fmt.Errorf("create plugin manager state: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write plugin manager state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close plugin manager state: %w", err)
	}
	if err := os.Rename(name, m.statePath); err != nil {
		return fmt.Errorf("publish plugin manager state: %w", err)
	}
	return nil
}
