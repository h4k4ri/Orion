package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/horizon/orion/sdk/go/plugin"
)

func main() {
	p := plugin.New(plugin.Config{
		ID:      "dns-plugin",
		Name:    "PowerDNS",
		Version: "1.0.0",
		Vendor:  "PowerDNS",
	})

	p.Resource("orion.io/dns.zone", "v1").
		Handle("create", handleZoneCreate).
		Handle("delete", handleZoneDelete).
		Handle("get", handleZoneGet).
		Handle("list", handleZoneList).
		Handle("update", handleZoneUpdate).
		AddCapability("dnssec", true).
		AddCapability("axfr", true).
		Register()

	p.Resource("orion.io/dns.record", "v1").
		Handle("create", handleRecordCreate).
		Handle("delete", handleRecordDelete).
		Handle("update", handleRecordUpdate).
		Handle("list", handleRecordList).
		Handle("get", handleRecordGet).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50058"
	}
	log.Printf("DNS plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func handleZoneCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	zoneType, _ := input["type"].(string)
	nameservers, _ := input["nameservers"].([]interface{})

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	if zoneType == "" {
		zoneType = "Native"
	}

	cmd := exec.CommandContext(ctx, "pdnsutil", "create-zone", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("DNS: create-zone failed: %s, %v", string(output), err)
	}

	for _, ns := range nameservers {
		nsStr, _ := ns.(string)
		if nsStr != "" {
			cmd := exec.CommandContext(ctx, "pdnsutil", "add-zone-key", name, nsStr, "KSK")
			cmd.CombinedOutput()
		}
	}

	log.Printf("DNS: created zone %s (type=%s)", name, zoneType)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"type":   zoneType,
			"status": "active",
		}),
	}, nil
}

func handleZoneDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	cmd := exec.CommandContext(ctx, "pdnsutil", "delete-zone", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("pdnsutil delete-zone failed: %w, %s", err, string(output))), nil
	}

	log.Printf("DNS: deleted zone %s", name)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleZoneGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	cmd := exec.CommandContext(ctx, "pdnsutil", "list-zone", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("pdnsutil list-zone failed: %w", err)), nil
	}

	log.Printf("DNS: got zone %s", name)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"zone":   string(output),
			"status": "active",
		}),
	}, nil
}

func handleZoneList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	cmd := exec.CommandContext(ctx, "pdnsutil", "list-zones")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("pdnsutil list-zones failed: %w", err)), nil
	}

	zones := strings.Split(strings.TrimSpace(string(output)), "\n")
	if zones[0] == "" {
		zones = []string{}
	}

	log.Printf("DNS: listed %d zones", len(zones))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"zones": zones,
		}),
	}, nil
}

func handleZoneUpdate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	log.Printf("DNS: updated zone %s", name)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"status": "updated",
		}),
	}, nil
}

func handleRecordCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	zone, _ := input["zone"].(string)
	name, _ := input["name"].(string)
	recordType, _ := input["type"].(string)
	value, _ := input["value"].(string)
	ttl, _ := input["ttl"].(float64)

	if zone == "" || name == "" || recordType == "" || value == "" {
		return errorResponse(fmt.Errorf("zone, name, type, and value are required")), nil
	}

	if ttl == 0 {
		ttl = 3600
	}

	fullName := name
	if !strings.HasSuffix(name, ".") {
		fullName = fmt.Sprintf("%s.%s.", name, zone)
	}

	cmd := exec.CommandContext(ctx, "pdnsutil", "add-record", zone, name, recordType, fmt.Sprintf("%v", ttl), value)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("pdnsutil add-record failed: %w, %s", err, string(output))), nil
	}

	recordID := fmt.Sprintf("%s %s %s", zone, name, recordType)
	log.Printf("DNS: created record %s", recordID)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":    recordID,
			"zone":  zone,
			"name":  fullName,
			"type":  recordType,
			"value": value,
			"ttl":   int(ttl),
		}),
	}, nil
}

func handleRecordDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	zone, _ := input["zone"].(string)
	name, _ := input["name"].(string)
	recordType, _ := input["type"].(string)

	if zone == "" || name == "" || recordType == "" {
		return errorResponse(fmt.Errorf("zone, name, and type are required")), nil
	}

	cmd := exec.CommandContext(ctx, "pdnsutil", "remove-record", zone, name, recordType)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("pdnsutil remove-record failed: %w, %s", err, string(output))), nil
	}

	log.Printf("DNS: deleted record %s %s %s", zone, name, recordType)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleRecordUpdate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	zone, _ := input["zone"].(string)
	name, _ := input["name"].(string)
	recordType, _ := input["type"].(string)
	value, _ := input["value"].(string)
	ttl, _ := input["ttl"].(float64)

	if zone == "" || name == "" || recordType == "" {
		return errorResponse(fmt.Errorf("zone, name, and type are required")), nil
	}

	var cmd *exec.Cmd
	if value != "" {
		if ttl > 0 {
			cmd = exec.CommandContext(ctx, "pdnsutil", "replace-record", zone, name, recordType, fmt.Sprintf("%v", ttl), value)
		} else {
			cmd = exec.CommandContext(ctx, "pdnsutil", "replace-record", zone, name, recordType, "3600", value)
		}
	} else {
		cmd = exec.CommandContext(ctx, "pdnsutil", "add-record", zone, name, recordType, "3600", value)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("pdnsutil replace-record failed: %w, %s", err, string(output))), nil
	}

	log.Printf("DNS: updated record %s %s %s", zone, name, recordType)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"zone":  zone,
			"name":  name,
			"type":  recordType,
			"value": value,
		}),
	}, nil
}

func handleRecordList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	zone, _ := input["zone"].(string)
	if zone == "" {
		return errorResponse(fmt.Errorf("zone is required")), nil
	}

	cmd := exec.CommandContext(ctx, "pdnsutil", "list-zone", zone)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("pdnsutil list-zone failed: %w", err)), nil
	}

	lines := strings.Split(string(output), "\n")
	var records []map[string]interface{}
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			records = append(records, map[string]interface{}{
				"name":  parts[0],
				"type":  parts[2],
				"ttl":   parts[1],
				"value": strings.Join(parts[3:], " "),
			})
		}
	}

	log.Printf("DNS: listed %d records in zone %s", len(records), zone)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"zone":    zone,
			"records": records,
		}),
	}, nil
}

func handleRecordGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	zone, _ := input["zone"].(string)
	name, _ := input["name"].(string)
	recordType, _ := input["type"].(string)

	if zone == "" || name == "" {
		return errorResponse(fmt.Errorf("zone and name are required")), nil
	}

	fullName := name
	if !strings.HasSuffix(name, ".") {
		fullName = fmt.Sprintf("%s.%s", name, zone)
	}

	cmd := exec.CommandContext(ctx, "dig", "+short", fullName, recordType)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("DNS: dig failed: %v", err)
	}

	log.Printf("DNS: got record %s %s", fullName, recordType)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"zone":  zone,
			"name":  fullName,
			"type":  recordType,
			"value": strings.TrimSpace(string(output)),
		}),
	}, nil
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "DNS_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
