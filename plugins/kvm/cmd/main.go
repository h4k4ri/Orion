//go:build libvirt

package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/horizon/orion/sdk/go/plugin"
	"libvirt.org/go/libvirt"
	"libvirt.org/go/libvirtxml"
)

type kvmPlugin struct {
	conn *libvirt.Connect
}

func main() {
	uri := os.Getenv("LIBVIRT_URI")
	if uri == "" {
		uri = "qemu:///system"
	}

	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		log.Fatalf("Failed to connect to libvirt at %s: %v", uri, err)
	}
	defer conn.Close()

	kvmp := &kvmPlugin{conn: conn}

	p := plugin.New(plugin.Config{
		ID:      "kvm-plugin",
		Name:    "KVM",
		Version: "1.0.0",
		Vendor:  "Linux/KVM",
	})

	p.Resource("orion.io/compute.instance", "v1").
		Handle("create", kvmp.handleInstanceCreate).
		Handle("delete", kvmp.handleInstanceDelete).
		Handle("start", kvmp.handleInstanceStart).
		Handle("stop", kvmp.handleInstanceStop).
		Handle("restart", kvmp.handleInstanceRestart).
		Handle("get", kvmp.handleInstanceGet).
		Handle("list", kvmp.handleInstanceList).
		Handle("resize", kvmp.handleInstanceResize).
		Handle("suspend", kvmp.handleInstanceSuspend).
		Handle("resume", kvmp.handleInstanceResume).
		Handle("migrate", kvmp.handleInstanceMigrate).
		AddCapability("suspend", true).
		AddCapability("live_migrate", true).
		AddCapability("snapshot", true).
		AddCapability("console", true).
		Register()

	p.Relationship("orion.io/storage.attachment", "target").
		Handle("attach", kvmp.handleStorageAttach).
		Handle("detach", kvmp.handleStorageDetach).
		Register()

	p.Relationship("orion.io/network.port", "target").
		Handle("attach", kvmp.handlePortAttach).
		Handle("detach", kvmp.handlePortDetach).
		Register()

	log.Printf("KVM plugin connected to libvirt at %s", uri)
	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50056"
	}
	log.Printf("KVM plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func (k *kvmPlugin) handleInstanceCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	flavor, _ := input["flavor"].(string)
	memory, _ := input["memory"].(float64)
	vcpu, _ := input["vcpu"].(float64)
	disk, _ := input["disk"].(string)
	network, _ := input["network"].(string)
	iso, _ := input["iso"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	if memory == 0 {
		memory = 2048
	}
	if vcpu == 0 {
		vcpu = 2
	}
	if disk == "" {
		disk = "/var/lib/libvirt/images/" + name + ".qcow2"
	}
	if network == "" {
		network = "default"
	}

	domCfg := &libvirtxml.Domain{
		Type:   "kvm",
		Memory: &libvirtxml.DomainMemory{Value: uint64(memory), Unit: "KiB"},
		VCPUs: &libvirtxml.DomainVCPUs{
			VCPU: []libvirtxml.DomainVCPU{{ID: 0, Enabled: true}},
		},
		Name: name,
		UUID: mustGenerateUUID(),
		OS: &libvirtxml.DomainOS{
			Type: &libvirtxml.DomainOSType{Type: "hvm"},
		},
		Devices: &libvirtxml.DomainDeviceList{
			Disks: []libvirtxml.DomainDisk{
				{
					Device: "disk",
					Source: &libvirtxml.DomainDiskSource{File: disk},
					Target: &libvirtxml.DomainDiskTarget{Dev: "vda", Bus: "virtio"},
					Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: "qcow2"},
				},
			},
			Interfaces: []libvirtxml.DomainInterface{
				{
					Source: &libvirtxml.DomainInterfaceSource{Network: &libvirtxml.DomainInterfaceSourceNetwork{Network: network}},
					Target: &libvirtxml.DomainInterfaceTarget{Dev: "vnet0"},
					Model:  &libvirtxml.DomainInterfaceModel{Type: "virtio"},
				},
			},
		},
	}

	if iso != "" {
		domCfg.Devices.Disks = append(domCfg.Devices.Disks, libvirtxml.DomainDisk{
			Device: "cdrom",
			Source: &libvirtxml.DomainDiskSource{File: iso},
			Target: &libvirtxml.DomainDiskTarget{Dev: "hda", Bus: "ide"},
		})
	}

	domXML, err := domCfg.Marshal()
	if err != nil {
		return errorResponse(fmt.Errorf("failed to marshal domain XML: %w", err)), nil
	}

	domain, err := k.conn.DomainDefineXML(domXML)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to define domain: %w", err)), nil
	}
	defer domain.Free()

	if err := domain.Create(); err != nil {
		return errorResponse(fmt.Errorf("failed to start domain: %w", err)), nil
	}

	instanceID := name
	log.Printf("KVM: created and started instance %s (memory=%d, vcpu=%d)", instanceID, int(memory), int(vcpu))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":         instanceID,
			"name":       name,
			"flavor":     flavor,
			"memory":     int(memory),
			"vcpu":       int(vcpu),
			"disk":       disk,
			"network":    network,
			"status":     "running",
			"hypervisor": "kvm",
		}),
	}, nil
}

func (k *kvmPlugin) handleInstanceDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	state, _, _ := domain.GetState()
	if state == libvirt.DomainRunning {
		if err := domain.Destroy(); err != nil {
			return errorResponse(fmt.Errorf("failed to destroy domain: %w", err)), nil
		}
	}

	if err := domain.Undefine(); err != nil {
		return errorResponse(fmt.Errorf("failed to undefine domain: %w", err)), nil
	}

	log.Printf("KVM: deleted instance %s", name)
	return &plugin.ResourceResponse{Success: true}, nil
}

func (k *kvmPlugin) handleInstanceStart(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	if err := domain.Create(); err != nil {
		return errorResponse(fmt.Errorf("failed to start domain: %w", err)), nil
	}

	log.Printf("KVM: started instance %s", name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "running"}),
	}, nil
}

func (k *kvmPlugin) handleInstanceStop(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	if err := domain.Shutdown(); err != nil {
		if err := domain.Destroy(); err != nil {
			return errorResponse(fmt.Errorf("failed to stop domain: %w", err)), nil
		}
	}

	log.Printf("KVM: stopped instance %s", name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "stopped"}),
	}, nil
}

func (k *kvmPlugin) handleInstanceRestart(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	if err := domain.Reboot(libvirt.DomainRebootDefault); err != nil {
		return errorResponse(fmt.Errorf("failed to reboot domain: %w", err)), nil
	}

	log.Printf("KVM: restarted instance %s", name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "running"}),
	}, nil
}

func (k *kvmPlugin) handleInstanceGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	state, _, err := domain.GetState()
	if err != nil {
		return errorResponse(fmt.Errorf("failed to get domain state: %w", err)), nil
	}

	xmlDesc, err := domain.GetXMLDesc(0)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to get domain XML: %w", err)), nil
	}

	var domCfg libvirtxml.Domain
	if err := xml.Unmarshal([]byte(xmlDesc), &domCfg); err != nil {
		return errorResponse(fmt.Errorf("failed to parse domain XML: %w", err)), nil
	}

	info, err := domain.GetInfo()
	if err != nil {
		return errorResponse(fmt.Errorf("failed to get domain info: %w", err)), nil
	}

	log.Printf("KVM: got instance %s (state=%s)", name, mapLibvirtState(state))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":         name,
			"name":       name,
			"status":     mapLibvirtState(state),
			"memory":     info.Memory,
			"max_memory": info.MaxMem,
			"vcpu":       info.NrVirtCpu,
			"cpu_time":   info.CPUTime,
			"hypervisor": "kvm",
		}),
	}, nil
}

func (k *kvmPlugin) handleInstanceList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	domains, err := k.conn.ListAllDomains(libvirt.ConnectListDomainsActive)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to list domains: %w", err)), nil
	}
	defer func() {
		for _, d := range domains {
			d.Free()
		}
	}()

	result := make([]map[string]interface{}, 0, len(domains))
	for _, domain := range domains {
		name, _ := domain.GetName()
		state, _, _ := domain.GetState()
		info, _ := domain.GetInfo()
		result = append(result, map[string]interface{}{
			"name":   name,
			"status": mapLibvirtState(state),
			"memory": info.Memory,
			"vcpu":   info.NrVirtCpu,
		})
	}

	log.Printf("KVM: listed %d active instances", len(result))
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"instances": result, "count": len(result)}),
	}, nil
}

func (k *kvmPlugin) handleInstanceResize(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	memory, _ := input["memory"].(float64)
	vcpu, _ := input["vcpu"].(float64)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	if memory > 0 {
		if err := domain.SetMemory(uint64(memory)); err != nil {
			return errorResponse(fmt.Errorf("failed to set memory: %w", err)), nil
		}
	}

	if vcpu > 0 {
		if err := domain.SetVCPUs(uint(vcpu)); err != nil {
			return errorResponse(fmt.Errorf("failed to set vcpu: %w", err)), nil
		}
	}

	log.Printf("KVM: resized instance %s (memory=%d, vcpu=%d)", name, int(memory), int(vcpu))
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "memory": int(memory), "vcpu": int(vcpu)}),
	}, nil
}

func (k *kvmPlugin) handleInstanceSuspend(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	if err := domain.PMSuspendForDuration(libvirt.DomainPMSuspendTarget, 0); err != nil {
		return errorResponse(fmt.Errorf("failed to suspend domain: %w", err)), nil
	}

	log.Printf("KVM: suspended instance %s", name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "suspended"}),
	}, nil
}

func (k *kvmPlugin) handleInstanceResume(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	if err := domain.PMWakeup(); err != nil {
		return errorResponse(fmt.Errorf("failed to resume domain: %w", err)), nil
	}

	log.Printf("KVM: resumed instance %s", name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "running"}),
	}, nil
}

func (k *kvmPlugin) handleInstanceMigrate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	destURI, _ := input["dest_uri"].(string)
	destHost, _ := input["dest_host"].(string)
	destPort, _ := input["dest_port"].(float64)

	if name == "" || destHost == "" {
		return errorResponse(fmt.Errorf("name and dest_host are required")), nil
	}
	if destURI == "" {
		destPortInt := 16509
		if destPort > 0 {
			destPortInt = int(destPort)
		}
		destURI = fmt.Sprintf("qemu+tcp://%s:%d/system", destHost, destPortInt)
	}

	domain, err := k.conn.LookupDomainByName(name)
	if err != nil {
		return errorResponse(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	flags := libvirt.DomainMigrateLive
	if err := domain.MigrateToURI(destURI, flags, ""); err != nil {
		return errorResponse(fmt.Errorf("failed to migrate domain: %w", err)), nil
	}

	log.Printf("KVM: migrated instance %s to %s", name, destHost)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "dest_host": destHost, "status": "migrated"}),
	}, nil
}

func (k *kvmPlugin) handleStorageAttach(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	sourceID := req.SourceResourceID
	targetID := req.TargetResourceID

	domain, err := k.conn.LookupDomainByName(targetID)
	if err != nil {
		return errorResponseRel(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	diskXML := fmt.Sprintf(`<disk type="block" device="disk"><source dev="%s"/><target dev="vdb" bus="virtio"/></disk>`, sourceID)
	if err := domain.AttachDevice(diskXML); err != nil {
		return errorResponseRel(fmt.Errorf("failed to attach disk: %w", err)), nil
	}

	log.Printf("KVM: attached storage %s to instance %s", sourceID, targetID)
	return &plugin.RelationshipResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"source_id": sourceID, "target_id": targetID, "device": "/dev/vdb"}),
	}, nil
}

func (k *kvmPlugin) handleStorageDetach(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	sourceID := req.SourceResourceID
	targetID := req.TargetResourceID

	domain, err := k.conn.LookupDomainByName(targetID)
	if err != nil {
		return errorResponseRel(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	diskXML := fmt.Sprintf(`<disk type="block" device="disk"><source dev="%s"/><target dev="vdb" bus="virtio"/></disk>`, sourceID)
	if err := domain.DetachDevice(diskXML); err != nil {
		return errorResponseRel(fmt.Errorf("failed to detach disk: %w", err)), nil
	}

	log.Printf("KVM: detached storage %s from instance %s", sourceID, targetID)
	return &plugin.RelationshipResponse{Success: true}, nil
}

func (k *kvmPlugin) handlePortAttach(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	sourceID := req.SourceResourceID
	targetID := req.TargetResourceID

	domain, err := k.conn.LookupDomainByName(targetID)
	if err != nil {
		return errorResponseRel(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	ifaceXML := fmt.Sprintf(`<interface type="network"><source network="%s"/><target dev="vnet1"/><model type="virtio"/></interface>`, sourceID)
	if err := domain.AttachDevice(ifaceXML); err != nil {
		return errorResponseRel(fmt.Errorf("failed to attach interface: %w", err)), nil
	}

	mac := fmt.Sprintf("52:54:00:%02x:%02x:%02x", time.Now().UnixNano()&0xff, (time.Now().UnixNano()>>8)&0xff, (time.Now().UnixNano()>>16)&0xff)

	log.Printf("KVM: attached port %s to instance %s (mac=%s)", sourceID, targetID, mac)
	return &plugin.RelationshipResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"source_id": sourceID, "target_id": targetID, "mac_address": mac, "bridge": "virbr0"}),
	}, nil
}

func (k *kvmPlugin) handlePortDetach(ctx context.Context, req *plugin.RelationshipRequest) (*plugin.RelationshipResponse, error) {
	sourceID := req.SourceResourceID
	targetID := req.TargetResourceID

	domain, err := k.conn.LookupDomainByName(targetID)
	if err != nil {
		return errorResponseRel(fmt.Errorf("domain not found: %w", err)), nil
	}
	defer domain.Free()

	ifaceXML := fmt.Sprintf(`<interface type="network"><source network="%s"/><target dev="vnet1"/><model type="virtio"/></interface>`, sourceID)
	if err := domain.DetachDevice(ifaceXML); err != nil {
		return errorResponseRel(fmt.Errorf("failed to detach interface: %w", err)), nil
	}

	log.Printf("KVM: detached port %s from instance %s", sourceID, targetID)
	return &plugin.RelationshipResponse{Success: true}, nil
}

func mapLibvirtState(state libvirt.DomainState) string {
	switch state {
	case libvirt.DomainRunning:
		return "running"
	case libvirt.DomainPaused:
		return "paused"
	case libvirt.DomainShutdown:
		return "shutdown"
	case libvirt.DomainShutoff:
		return "stopped"
	case libvirt.DomainCrashed:
		return "crashed"
	case libvirt.DomainPMSuspended:
		return "suspended"
	default:
		return "unknown"
	}
}

func mustGenerateUUID() string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		time.Now().UnixNano()&0xffffffff,
		time.Now().UnixNano()>>32&0xffff,
		time.Now().UnixNano()>>48&0xffff,
		time.Now().UnixNano()>>60&0xffff,
		time.Now().UnixNano())
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "KVM_ERROR", Message: err.Error()}}
}

func errorResponseRel(err error) *plugin.RelationshipResponse {
	return &plugin.RelationshipResponse{Success: false, Error: &plugin.Error{Code: "KVM_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
