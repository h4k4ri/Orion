package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ManifestBuilder struct {
	manifest map[string]interface{}
}

func NewManifest(cfg Config) *ManifestBuilder {
	return &ManifestBuilder{
		manifest: map[string]interface{}{
			"apiVersion": "orion.io/v1",
			"pluginId":   cfg.ID,
			"name":       cfg.Name,
			"version":    cfg.Version,
			"vendor":     cfg.Vendor,
			"runtime": map[string]string{
				"protocol":        "grpc",
				"protocolVersion": "v1",
			},
			"implements":              []interface{}{},
			"implementsRelationships": []interface{}{},
		},
	}
}

func (mb *ManifestBuilder) AddResource(kind, version string, ops []string, capabilities map[string]interface{}) *ManifestBuilder {
	resource := map[string]interface{}{
		"kind":         kind,
		"version":      version,
		"operations":   ops,
		"capabilities": capabilities,
	}

	impl := mb.manifest["implements"].([]interface{})
	mb.manifest["implements"] = append(impl, resource)
	return mb
}

func (mb *ManifestBuilder) AddRelationship(kind, version, role string, ops []string) *ManifestBuilder {
	relationship := map[string]interface{}{
		"kind":       kind,
		"version":    version,
		"role":       role,
		"operations": ops,
	}

	impl := mb.manifest["implementsRelationships"].([]interface{})
	mb.manifest["implementsRelationships"] = append(impl, relationship)
	return mb
}

func (mb *ManifestBuilder) Build() map[string]interface{} {
	return mb.manifest
}

// ManifestJSON serializes the manifest using stable indentation for humans and
// for tools that inspect installed plugins.
func (mb *ManifestBuilder) ManifestJSON() ([]byte, error) {
	return json.MarshalIndent(mb.manifest, "", "  ")
}

// WriteManifest writes the manifest atomically, so a plugin manager never
// observes a partially-written file during discovery or startup.
func (mb *ManifestBuilder) WriteManifest(path string) error {
	if path == "" {
		return fmt.Errorf("manifest path is required")
	}
	data, err := mb.ManifestJSON()
	if err != nil {
		return fmt.Errorf("marshal plugin manifest: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary manifest: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("set manifest permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close manifest: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publish manifest: %w", err)
	}
	return nil
}

// WriteManifest publishes the current plugin registration as a manifest.
func (p *Plugin) WriteManifest(path string) error {
	return (&ManifestBuilder{manifest: p.Manifest()}).WriteManifest(path)
}
