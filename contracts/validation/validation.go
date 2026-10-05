package validation

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/horizon/orion/contracts/specs/v1/resources"
	"github.com/horizon/orion/contracts/specs/v1/relationships"
)

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

type Validator struct{}

func NewValidator() *Validator {
	return &Validator{}
}

func (v *Validator) ValidatePluginManifest(manifest interface{}) []ValidationError {
	var errors []ValidationError
	if manifest == nil || (reflect.ValueOf(manifest).Kind() == reflect.Ptr && reflect.ValueOf(manifest).IsNil()) {
		return []ValidationError{{Field: "manifest", Message: "manifest is required"}}
	}

	encoded, err := json.Marshal(manifest)
	if err != nil {
		return []ValidationError{{Field: "manifest", Message: fmt.Sprintf("cannot encode manifest: %v", err)}}
	}
	var normalized struct {
		APIVersion string `json:"apiVersion"`
		PluginID string `json:"pluginId"`
		Name string `json:"name"`
		Version string `json:"version"`
		Runtime struct {
			Protocol string `json:"protocol"`
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"runtime"`
		Implements []struct {
			Kind string `json:"kind"`
			Version string `json:"version"`
			Operations []string `json:"operations"`
			Capabilities map[string]interface{} `json:"capabilities"`
		} `json:"implements"`
		Relationships []struct {
			Kind string `json:"kind"`
			Version string `json:"version"`
			Role string `json:"role"`
			Operations []string `json:"operations"`
		} `json:"implementsRelationships"`
	}
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return []ValidationError{{Field: "manifest", Message: fmt.Sprintf("invalid manifest: %v", err)}}
	}
	if normalized.PluginID == "" { errors = append(errors, ValidationError{"pluginId", "is required"}) }
	if normalized.Name == "" { errors = append(errors, ValidationError{"name", "is required"}) }
	if normalized.Version == "" { errors = append(errors, ValidationError{"version", "is required"}) }
	if normalized.APIVersion != "" && normalized.APIVersion != "orion.io/v1" { errors = append(errors, ValidationError{"apiVersion", "must be orion.io/v1"}) }
	if normalized.Runtime.Protocol != "" && normalized.Runtime.Protocol != "grpc" { errors = append(errors, ValidationError{"runtime.protocol", "must be grpc"}) }
	if normalized.Runtime.ProtocolVersion != "" && normalized.Runtime.ProtocolVersion != "v1" { errors = append(errors, ValidationError{"runtime.protocolVersion", "must be v1"}) }

	implementedKinds := make([]string, 0, len(normalized.Implements))
	for i, implementation := range normalized.Implements {
		field := fmt.Sprintf("implements[%d]", i)
		if implementation.Kind == "" { errors = append(errors, ValidationError{field+".kind", "is required"}); continue }
		if err := v.ValidateResourceSpec(implementation.Kind, implementation.Version); err != nil { errors = append(errors, ValidationError{field, err.Error()}) }
		if len(implementation.Operations) == 0 { errors = append(errors, ValidationError{field+".operations", "must contain at least one operation"}) }
		implementedKinds = append(implementedKinds, implementation.Kind)
	}
	for _, implementation := range normalized.Implements {
		errors = append(errors, v.ValidateCapabilityDependencies(implementation.Kind, implementation.Capabilities, implementedKinds)...)
	}
	for i, relationship := range normalized.Relationships {
		field := fmt.Sprintf("implementsRelationships[%d]", i)
		if err := v.ValidateRelationshipSpec(relationship.Kind, relationship.Version); err != nil { errors = append(errors, ValidationError{field, err.Error()}) }
		if relationship.Role != "source" && relationship.Role != "target" { errors = append(errors, ValidationError{field+".role", "must be source or target"}) }
		if len(relationship.Operations) == 0 { errors = append(errors, ValidationError{field+".operations", "must contain at least one operation"}) }
	}
	return errors
}

func (v *Validator) ValidateResourceSpec(kind, version string) error {
	if !knownResourceKind(kind) {
		return fmt.Errorf("unknown resource kind: %s", kind)
	}
	if version != "v1" { return fmt.Errorf("unsupported version for %s: %s", kind, version) }
	return nil
}

func (v *Validator) ValidateRelationshipSpec(kind, version string) error {
	if !knownRelationshipKind(kind) {
		return fmt.Errorf("unknown relationship kind: %s", kind)
	}
	if version != "v1" { return fmt.Errorf("unsupported version for %s: %s", kind, version) }
	return nil
}

func knownResourceKind(kind string) bool {
	switch kind {
	case resources.StorageVolumeKind, resources.StorageSnapshotKind, resources.StorageFilesystemKind, resources.StorageObjectKind, resources.StorageShareKind,
		resources.ComputeInstanceKind, resources.ComputeHostKind, resources.ComputeBaremetalKind,
		resources.NetworkNetworkKind, resources.NetworkSubnetKind, resources.NetworkPortKind, resources.NetworkRouterKind, resources.NetworkFloatingIpKind,
		resources.LoadbalancerLoadbalancerKind, resources.LoadbalancerListenerKind, resources.LoadbalancerPoolKind, resources.LoadbalancerMemberKind, resources.LoadbalancerHealthmonitorKind,
		resources.SecretSecretKind, resources.SecretKeyKind, resources.SecretCertificateKind, resources.DNSZoneKind, resources.DNSRecordKind,
		resources.ImageImageKind, resources.BackupJobKind, resources.DatabaseInstanceKind, resources.MonitoringMetricKind, resources.MonitoringAlarmKind, resources.HAProtectionKind:
		return true
	default:
		return false
	}
}

func knownRelationshipKind(kind string) bool {
	switch kind {
	case relationships.StorageAttachmentKind, relationships.NetworkPortAttachmentKind, relationships.NetworkFloatingIpBindingKind,
		relationships.LoadbalancerMembershipKind, relationships.SecretAttachmentKind, relationships.LoadbalancerCertificateBindingKind:
		return true
	default:
		return false
	}
}

func (v *Validator) ValidateCapabilityDependencies(
	kind string,
	capabilities map[string]interface{},
	implementedKinds []string,
) []ValidationError {
	var errors []ValidationError

	// Check snapshots: true requires storage.snapshot implementation
	if snapshots, ok := capabilities["snapshots"]; ok {
		if snapshots == true {
			hasSnapshotImpl := false
			for _, k := range implementedKinds {
				if k == "orion.io/storage.snapshot" {
					hasSnapshotImpl = true
					break
				}
			}
			if !hasSnapshotImpl {
				errors = append(errors, ValidationError{
					Field:   "capabilities.snapshots",
					Message: "snapshots: true requires implementation of orion.io/storage.snapshot",
				})
			}
		}
	}

	return errors
}
