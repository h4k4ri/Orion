package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/horizon/orion/sdk/go/plugin"
)

type ConformanceTest struct {
	Name    string
	Handler interface{}
}

func RunConformanceSuite(t *testing.T, pluginName string, p *plugin.Plugin) {
	t.Run("Manifest", func(t *testing.T) {
		testManifestValid(t, p)
	})

	t.Run("Resources", func(t *testing.T) {
		testResourceHandlers(t, p)
	})

	t.Run("Relationships", func(t *testing.T) {
		testRelationshipHandlers(t, p)
	})
}

func testManifestValid(t *testing.T, p *plugin.Plugin) {
	if p == nil {
		t.Error("Plugin is nil")
		return
	}

	cfg := p.GetConfig()
	if cfg.ID == "" {
		t.Error("Plugin ID is empty")
	}
	if cfg.Name == "" {
		t.Error("Plugin Name is empty")
	}
	if cfg.Version == "" {
		t.Error("Plugin Version is empty")
	}
}

func testResourceHandlers(t *testing.T, p *plugin.Plugin) {
	resources := p.ListResources()

	for _, res := range resources {
		t.Run(res.Kind, func(t *testing.T) {
			if len(res.Ops) == 0 {
				t.Errorf("Resource %s has no operations", res.Kind)
			}

			for opName := range res.Ops {
				t.Run(opName, func(t *testing.T) {
					if opName == "" {
						t.Error("Operation name is empty")
					}
				})
			}
		})
	}
}

func testRelationshipHandlers(t *testing.T, p *plugin.Plugin) {
	relationships := p.ListRelationships()

	for _, rel := range relationships {
		t.Run(rel.Kind+":"+rel.Role, func(t *testing.T) {
			if len(rel.Ops) == 0 {
				t.Errorf("Relationship %s (role %s) has no operations", rel.Kind, rel.Role)
			}

			for opName := range rel.Ops {
				t.Run(opName, func(t *testing.T) {
					if opName == "" {
						t.Error("Operation name is empty")
					}
				})
			}
		})
	}
}

type PluginUnderTest struct {
	Plugin *plugin.Plugin
	Name   string
}

func RunAllConformanceTests(t *testing.T, plugins []PluginUnderTest) {
	for _, p := range plugins {
		t.Run(p.Name, func(t *testing.T) {
			RunConformanceSuite(t, p.Name, p.Plugin)
		})
	}
}

func ValidateManifest(manifest *plugin.Plugin) error {
	if manifest == nil {
		return fmt.Errorf("manifest is nil")
	}

	cfg := manifest.GetConfig()
	if cfg.ID == "" {
		return fmt.Errorf("plugin ID is required")
	}
	if cfg.Name == "" {
		return fmt.Errorf("plugin name is required")
	}
	if cfg.Version == "" {
		return fmt.Errorf("plugin version is required")
	}

	return nil
}

func ValidateResourceKind(kind, version string, ops []string) error {
	if kind == "" {
		return fmt.Errorf("resource kind is required")
	}
	if version == "" {
		return fmt.Errorf("resource version is required")
	}
	if len(ops) == 0 {
		return fmt.Errorf("resource must have at least one operation")
	}
	return nil
}

func ValidateRelationshipKind(kind, version, role string, ops []string) error {
	if kind == "" {
		return fmt.Errorf("relationship kind is required")
	}
	if version == "" {
		return fmt.Errorf("relationship version is required")
	}
	if role == "" {
		return fmt.Errorf("relationship role is required")
	}
	if role != "source" && role != "target" {
		return fmt.Errorf("relationship role must be 'source' or 'target', got: %s", role)
	}
	if len(ops) == 0 {
		return fmt.Errorf("relationship must have at least one operation")
	}
	return nil
}

func ValidateOperationResult(result *plugin.ResourceResponse) error {
	if result == nil {
		return fmt.Errorf("operation result is nil")
	}
	return nil
}

func ValidateError(err *plugin.Error) error {
	if err == nil {
		return nil
	}
	if err.Code == "" {
		return fmt.Errorf("error code is required when error is present")
	}
	if err.Message == "" {
		return fmt.Errorf("error message is required when error is present")
	}
	return nil
}

type Capability string

const (
	CapabilitySnapshots Capability = "snapshots"
	CapabilityEncryption Capability = "encryption"
	CapabilityBackup    Capability = "backup"
)

func ValidateCapabilities(resourceKind string, capabilities map[string]interface{}) error {
	switch resourceKind {
	case "orion.io/storage.volume":
		if snapshots, ok := capabilities["snapshots"]; ok {
			if snapshots == true {
				return fmt.Errorf("storage.volume with snapshots=true requires storage.snapshot implementation")
			}
		}
	}
	return nil
}

func ValidateIdempotency(ctx context.Context, handler plugin.ResourceOperationHandler, input []byte) error {
	resp1, err := handler(ctx, &plugin.ResourceRequest{Payload: input})
	if err != nil {
		return err
	}

	resp2, err := handler(ctx, &plugin.ResourceRequest{Payload: input})
	if err != nil {
		return err
	}

	if !resp1.Success || !resp2.Success {
		return nil
	}

	var result1, result2 interface{}
	json.Unmarshal(resp1.Result, &result1)
	json.Unmarshal(resp2.Result, &result2)

	if fmt.Sprintf("%v", result1) != fmt.Sprintf("%v", result2) {
		return fmt.Errorf("operation is not idempotent: different results for same input")
	}

	return nil
}
