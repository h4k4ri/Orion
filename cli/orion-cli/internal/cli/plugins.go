package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/horizon/orion/libs/go/kit/pluginmanager"
)

type pluginGroup struct {
	Package        pluginPackageCmd        `cmd:"" help:"Package a plugin executable."`
	Install        pluginInstallCmd        `cmd:"" help:"Install a plugin executable."`
	InstallPackage pluginInstallPackageCmd `cmd:"" help:"Install an .orion-plugin.tar.gz package."`
	Discover       pluginDiscoverCmd       `cmd:"" help:"Discover *.plugin.json descriptors."`
	List           pluginListCmd           `cmd:"" help:"List installed plugins."`
	Start          pluginStartCmd          `cmd:"" help:"Start a plugin and allocate an endpoint."`
	Stop           pluginStopCmd           `cmd:"" help:"Stop a running plugin."`
	Uninstall      pluginUninstallCmd      `cmd:"" help:"Remove an installed plugin."`
	Doctor         pluginDoctorCmd         `cmd:"" help:"Check plugin binary, manifest, and endpoint."`
}

type pluginPackageCmd struct {
	ID      string   `name:"id" required:"" help:"Stable plugin identifier."`
	Name    string   `name:"name" required:"" help:"Human-readable plugin name."`
	Version string   `name:"version" required:"" help:"Plugin version."`
	Vendor  string   `name:"vendor" default:"community" help:"Plugin vendor."`
	Env     []string `name:"env" help:"Environment override in KEY=VALUE form."`
	Binary  string   `name:"binary" required:"" help:"Plugin executable path."`
	Output  string   `name:"output" required:"" help:"Output .orion-plugin.tar.gz path."`
}

func (c *pluginPackageCmd) Run() error {
	env, err := parsePluginEnv(c.Env)
	if err != nil {
		return err
	}
	spec := pluginmanager.Spec{ID: c.ID, Name: c.Name, Version: c.Version, Vendor: c.Vendor, Binary: c.Binary, Env: env}
	if err := pluginmanager.CreatePackage(spec, c.Output); err != nil {
		return err
	}
	fmt.Printf("plugin package created: %s\n", c.Output)
	return nil
}

type pluginInstallCmd struct {
	ID       string   `name:"id" required:"" help:"Stable plugin identifier."`
	Name     string   `name:"name" required:"" help:"Human-readable plugin name."`
	Version  string   `name:"version" required:"" help:"Plugin version."`
	Vendor   string   `name:"vendor" default:"community" help:"Plugin vendor."`
	Endpoint string   `name:"endpoint" help:"Optional fixed host:port; empty allocates a free local port."`
	Env      []string `name:"env" help:"Environment override in KEY=VALUE form."`
	Binary   string   `arg:"" help:"Plugin executable path."`
}

func (c *pluginInstallCmd) Run() error {
	env, err := parsePluginEnv(c.Env)
	if err != nil {
		return err
	}
	m, err := newPluginManager()
	if err != nil {
		return err
	}
	if err := m.Install(pluginmanager.Spec{ID: c.ID, Name: c.Name, Version: c.Version, Vendor: c.Vendor, Binary: c.Binary, Endpoint: c.Endpoint, Env: env}); err != nil {
		return err
	}
	fmt.Printf("plugin %s installed\n", c.ID)
	return nil
}

type pluginInstallPackageCmd struct {
	Package string `arg:"" required:"" help:"Path to an .orion-plugin.tar.gz package."`
}

func (c *pluginInstallPackageCmd) Run() error {
	m, err := newPluginManager()
	if err != nil {
		return err
	}
	spec, err := m.InstallPackage(c.Package)
	if err != nil {
		return err
	}
	fmt.Printf("plugin %s installed from package\n", spec.ID)
	return nil
}

type pluginDiscoverCmd struct {
	Directory string `arg:"" help:"Directory containing plugin descriptors."`
}

func (c *pluginDiscoverCmd) Run() error {
	specs, err := pluginmanager.Discover(c.Directory)
	if err != nil {
		return err
	}
	m, err := newPluginManager()
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if err := m.Install(spec); err != nil {
			return err
		}
		fmt.Printf("plugin %s installed\n", spec.ID)
	}
	if len(specs) == 0 {
		fmt.Println("no plugin descriptors found")
	}
	return nil
}

type pluginListCmd struct{}

func (c *pluginListCmd) Run() error {
	m, err := newPluginManager()
	if err != nil {
		return err
	}
	return printPluginJSON(m.List())
}

type pluginStartCmd struct {
	ID string `arg:"" required:""`
}

func (c *pluginStartCmd) Run() error {
	m, err := newPluginManager()
	if err != nil {
		return err
	}
	if err := m.Start(context.Background(), c.ID); err != nil {
		return err
	}
	for _, state := range m.List() {
		if state.Spec.ID == c.ID {
			fmt.Printf("plugin %s running at %s\n", c.ID, state.Endpoint)
			return nil
		}
	}
	return nil
}

type pluginStopCmd struct {
	ID string `arg:"" required:""`
}

func (c *pluginStopCmd) Run() error {
	m, err := newPluginManager()
	if err != nil {
		return err
	}
	if err := m.Stop(context.Background(), c.ID); err != nil {
		return err
	}
	fmt.Printf("plugin %s stopped\n", c.ID)
	return nil
}

type pluginUninstallCmd struct {
	ID string `arg:"" required:""`
}

func (c *pluginUninstallCmd) Run() error {
	m, err := newPluginManager()
	if err != nil {
		return err
	}
	if err := m.Uninstall(c.ID); err != nil {
		return err
	}
	fmt.Printf("plugin %s uninstalled\n", c.ID)
	return nil
}

type pluginDoctorCmd struct {
	ID string `arg:"" required:""`
}

func (c *pluginDoctorCmd) Run() error {
	m, err := newPluginManager()
	if err != nil {
		return err
	}
	diagnostic, err := m.Doctor(context.Background(), c.ID)
	if err != nil {
		return err
	}
	return printPluginJSON(diagnostic)
}

func newPluginManager() (*pluginmanager.Manager, error) {
	path := os.Getenv("ORION_PLUGIN_STATE")
	if path == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve Orion config directory: %w", err)
		}
		path = filepath.Join(configDir, "orion", "plugins.json")
	}
	return pluginmanager.New(path)
}

func parsePluginEnv(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid plugin environment %q; expected KEY=VALUE", value)
		}
		result[key] = val
	}
	return result, nil
}

func printPluginJSON(value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
