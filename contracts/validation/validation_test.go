package validation

import "testing"

func TestValidatePluginManifest(t *testing.T) {
	validator := NewValidator()
	manifest := map[string]interface{}{
		"apiVersion": "orion.io/v1",
		"pluginId":   "zfs-plugin",
		"name":       "ZFS",
		"version":    "1.0.0",
		"runtime":    map[string]interface{}{"protocol": "grpc", "protocolVersion": "v1"},
		"implements": []interface{}{
			map[string]interface{}{"kind": "orion.io/storage.volume", "version": "v1", "operations": []string{"create"}, "capabilities": map[string]interface{}{"snapshots": true}},
			map[string]interface{}{"kind": "orion.io/storage.snapshot", "version": "v1", "operations": []string{"create"}},
		},
	}
	if errors := validator.ValidatePluginManifest(manifest); len(errors) != 0 {
		t.Fatalf("valid manifest rejected: %v", errors)
	}
}

func TestValidatePluginManifestRejectsMissingSnapshotDependency(t *testing.T) {
	validator := NewValidator()
	manifest := map[string]interface{}{
		"pluginId": "broken",
		"name":     "Broken",
		"version":  "1.0.0",
		"implements": []interface{}{
			map[string]interface{}{"kind": "orion.io/storage.volume", "version": "v1", "operations": []string{"create"}, "capabilities": map[string]interface{}{"snapshots": true}},
		},
	}
	if errors := validator.ValidatePluginManifest(manifest); len(errors) == 0 {
		t.Fatal("expected missing snapshot dependency to be rejected")
	}
}
