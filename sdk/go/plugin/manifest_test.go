package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginManifestIncludesRegistrations(t *testing.T) {
	p := New(Config{ID: "test-plugin", Name: "Test", Version: "1.2.3", Vendor: "Orion"})
	p.Resource("orion.io/test.resource", "v1").
		Handle("z", func(context.Context, *ResourceRequest) (*ResourceResponse, error) { return nil, nil }).
		Handle("a", func(context.Context, *ResourceRequest) (*ResourceResponse, error) { return nil, nil }).
		AddCapability("snapshots", true).
		Register()
	p.Relationship("orion.io/test.relationship", "target").
		Handle("attach", func(context.Context, *RelationshipRequest) (*RelationshipResponse, error) { return nil, nil }).
		Register()

	manifest := p.Manifest()
	if manifest["pluginId"] != "test-plugin" {
		t.Fatalf("unexpected plugin id: %#v", manifest["pluginId"])
	}
	resources := manifest["implements"].([]interface{})
	if len(resources) != 1 {
		t.Fatalf("expected one resource, got %d", len(resources))
	}
	resource := resources[0].(map[string]interface{})
	if got := resource["operations"].([]string); len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("operations are not deterministic: %#v", got)
	}
	if len(manifest["implementsRelationships"].([]interface{})) != 1 {
		t.Fatal("expected one relationship")
	}
}

func TestWriteManifestIsValidJSON(t *testing.T) {
	p := New(Config{ID: "test-plugin", Name: "Test", Version: "1.0.0", Vendor: "Orion"})
	path := filepath.Join(t.TempDir(), "nested", "plugin.json")
	if err := p.WriteManifest(path); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("manifest is invalid JSON: %v", err)
	}
}
