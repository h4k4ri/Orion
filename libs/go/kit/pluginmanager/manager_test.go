package pluginmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallPersistsAndListsPlugin(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "config", "plugins.json")
	m, err := New(statePath)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{ID: "demo", Name: "Demo", Version: "1.0.0", Binary: "/bin/true"}
	if err := m.Install(spec); err != nil {
		t.Fatal(err)
	}
	list := m.List()
	if len(list) != 1 || list[0].Spec.ID != "demo" || list[0].Status != "stopped" {
		t.Fatalf("unexpected plugin list: %#v", list)
	}
	reloaded, err := New(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 1 {
		t.Fatalf("plugin was not persisted: %#v", reloaded.List())
	}
}

func TestDiscoverDescriptors(t *testing.T) {
	dir := t.TempDir()
	descriptor := Spec{ID: "demo", Name: "Demo", Version: "1.0.0", Binary: "demo"}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo.plugin.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "demo" {
		t.Fatalf("unexpected discovery result: %#v", got)
	}
	if got[0].Binary != filepath.Join(dir, "demo") {
		t.Fatalf("relative binary was not resolved: %s", got[0].Binary)
	}
}

func TestCreateAndInstallPackage(t *testing.T) {
	dir := t.TempDir()
	packagePath := filepath.Join(dir, "demo.orion-plugin.tar.gz")
	spec := Spec{ID: "demo", Name: "Demo", Version: "1.0.0", Vendor: "Orion", Binary: "/bin/true"}
	if err := CreatePackage(spec, packagePath); err != nil {
		t.Fatal(err)
	}
	m, err := New(filepath.Join(dir, "plugins.json"))
	if err != nil {
		t.Fatal(err)
	}
	installed, err := m.InstallPackage(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ID != spec.ID || installed.Binary == spec.Binary {
		t.Fatalf("unexpected installed package: %#v", installed)
	}
	if _, err := os.Stat(installed.Binary); err != nil {
		t.Fatalf("installed binary is missing: %v", err)
	}
	if list := m.List(); len(list) != 1 || list[0].Spec.ID != "demo" {
		t.Fatalf("package was not registered: %#v", list)
	}
}
