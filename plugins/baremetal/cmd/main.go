package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-chi/chi/v5"

	"github.com/horizon/orion/plugins/baremetal/bmc"
	"github.com/horizon/orion/plugins/baremetal/driver"
	"github.com/horizon/orion/sdk/go/httpapi"
	"github.com/horizon/orion/sdk/go/plugin"
	"github.com/horizon/orion/sdk/go/schema"
)

var (
	_defaultPort = 50070
	_drivers     = sync.Map{}
	_validator   = schema.NewValidator()
	_server      *http.Server
)

func main() {
	p := plugin.New(plugin.Config{ID: "baremetal-plugin", Name: "Orion Bare Metal", Version: "1.0.0", Vendor: "Orion"})
	p.Resource("orion.io/compute.baremetal", "v1").
		Handle("create", adapt(handleCreate)).
		Handle("get", adapt(handleGet)).
		Handle("delete", adapt(handleDelete)).
		Handle("power_on", adapt(handlePowerOn)).
		Handle("power_off", adapt(handlePowerOff)).
		Handle("reboot", adapt(handleReboot)).
		Handle("set_boot_device", adapt(handleSetBootDevice)).
		Handle("get_inventory", adapt(handleGetInventory)).
		Handle("provision", adapt(handleProvision)).
		Handle("deprovision", adapt(handleDeprovision)).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = fmt.Sprintf(":%d", _defaultPort)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := p.Serve(ctx, endpoint); err != nil {
		panic(fmt.Errorf("baremetal plugin stopped: %w", err))
	}
}

func adapt(handler func(context.Context, *plugin.ResourceRequest) *plugin.ResourceResponse) plugin.ResourceOperationHandler {
	return func(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
		return handler(ctx, req), nil
	}
}

func httpAPIHandleCreate(w http.ResponseWriter, r *http.Request) {
	body, err := httpapi.ParseJSONBody(r)
	if err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "INVALID_BODY", err.Error())
		return
	}

	req := &plugin.ResourceRequest{Payload: body}
	resp := handleCreate(r.Context(), req)

	if !resp.Success {
		httpapi.WriteError(w, http.StatusInternalServerError, "CREATE_FAILED", responseErrorMessage(resp.Error))
		return
	}

	httpapi.WriteSuccess(w, http.StatusCreated, resp.Result)
}

func httpAPIHandleGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "MISSING_NAME", "node name is required")
		return
	}

	req := &plugin.ResourceRequest{
		Payload: marshal(map[string]interface{}{"name": name}),
	}
	resp := handleGet(r.Context(), req)

	if !resp.Success {
		httpapi.WriteError(w, http.StatusNotFound, "NOT_FOUND", responseErrorMessage(resp.Error))
		return
	}

	httpapi.WriteSuccess(w, http.StatusOK, resp.Result)
}

func httpAPIHandleDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "MISSING_NAME", "node name is required")
		return
	}

	req := &plugin.ResourceRequest{
		Payload: marshal(map[string]interface{}{"name": name}),
	}
	resp := handleDelete(r.Context(), req)

	if !resp.Success {
		httpapi.WriteError(w, http.StatusInternalServerError, "DELETE_FAILED", responseErrorMessage(resp.Error))
		return
	}

	httpapi.WriteSuccess(w, http.StatusOK, resp.Result)
}

func httpAPIHandlePower(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "MISSING_NAME", "node name is required")
		return
	}

	body, _ := httpapi.ParseJSONBody(r)
	var input map[string]interface{}
	json.Unmarshal(body, &input)

	action := getString(input, "action")
	req := &plugin.ResourceRequest{
		Payload: marshal(map[string]interface{}{"name": name}),
	}

	var resp *plugin.ResourceResponse
	switch action {
	case "on":
		resp = handlePowerOn(r.Context(), req)
	case "off":
		resp = handlePowerOff(r.Context(), req)
	case "reboot":
		resp = handleReboot(r.Context(), req)
	default:
		httpapi.WriteError(w, http.StatusBadRequest, "INVALID_ACTION", "action must be: on, off, or reboot")
		return
	}

	if !resp.Success {
		httpapi.WriteError(w, http.StatusInternalServerError, "POWER_FAILED", responseErrorMessage(resp.Error))
		return
	}

	httpapi.WriteSuccess(w, http.StatusOK, resp.Result)
}

func httpAPIHandleBootDevice(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		httpapi.WriteError(w, http.StatusBadRequest, "MISSING_NAME", "node name is required")
		return
	}

	body, _ := httpapi.ParseJSONBody(r)
	var input map[string]interface{}
	json.Unmarshal(body, &input)

	input["name"] = name
	req := &plugin.ResourceRequest{Payload: marshal(input)}
	resp := handleSetBootDevice(r.Context(), req)

	if !resp.Success {
		httpapi.WriteError(w, http.StatusInternalServerError, "BOOT_DEVICE_FAILED", responseErrorMessage(resp.Error))
		return
	}

	httpapi.WriteSuccess(w, http.StatusOK, resp.Result)
}

func handleCreate(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err)
	}

	name := getString(input, "name")
	protocol := getString(input, "protocol")
	if protocol == "" {
		protocol = "redfish"
	}

	if name == "" {
		return errorResponse(fmt.Errorf("name is required"))
	}

	cfg := extractDriverConfig(input)
	drv, err := driver.NewDriver(protocol, cfg)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to create driver: %w", err))
	}

	initialState, err := drv.GetPowerState(ctx)
	if err != nil {
		drv.Close(ctx)
		return errorResponse(fmt.Errorf("failed to connect to BMC: %w", err))
	}

	_drivers.Store(name, drv)

	inv, _ := drv.GetInventory(ctx)

	result := map[string]interface{}{
		"id":          name,
		"name":        name,
		"protocol":    protocol,
		"power_state": string(initialState),
		"status":      "enrolling",
	}

	if inv != nil {
		result["manufacturer"] = inv.Manufacturer
		result["model"] = inv.Model
		result["serial"] = inv.SerialNumber
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(result),
	}
}

func handleGet(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err)
	}

	name := getString(input, "id")
	if name == "" {
		name = getString(input, "name")
	}
	if name == "" {
		return errorResponse(fmt.Errorf("id or name required"))
	}

	drv, ok := _drivers.Load(name)
	if !ok {
		return errorResponse(fmt.Errorf("node not found: %s", name))
	}

	d := drv.(bmc.Driver)

	powerState, err := d.GetPowerState(ctx)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to get power state: %w", err))
	}

	inv, err := d.GetInventory(ctx)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to get inventory: %w", err))
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":            name,
			"power_state":   string(powerState),
			"manufacturer":  inv.Manufacturer,
			"model":         inv.Model,
			"serial":        inv.SerialNumber,
			"cpu_count":     inv.CPUCount,
			"memory_bytes":  inv.MemoryBytes,
			"nic_count":     inv.NICCount,
			"storage_count": inv.StorageCount,
		}),
	}
}

func handleDelete(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err)
	}

	name := getString(input, "id")
	if name == "" {
		name = getString(input, "name")
	}

	drv, ok := _drivers.LoadAndDelete(name)
	if !ok {
		return errorResponse(fmt.Errorf("node not found: %s", name))
	}

	d := drv.(bmc.Driver)
	d.Close(ctx)

	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "deleted"}),
	}
}

func handlePowerOn(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	return doPowerAction(ctx, req, func(d bmc.Driver) error {
		return d.PowerOn(ctx)
	})
}

func handlePowerOff(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	return doPowerAction(ctx, req, func(d bmc.Driver) error {
		return d.PowerOff(ctx)
	})
}

func handleReboot(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	return doPowerAction(ctx, req, func(d bmc.Driver) error {
		return d.Reboot(ctx)
	})
}

func handleSetBootDevice(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err)
	}

	name := getString(input, "id")
	if name == "" {
		name = getString(input, "name")
	}
	if name == "" {
		return errorResponse(fmt.Errorf("id or name required"))
	}

	drv, ok := _drivers.Load(name)
	if !ok {
		return errorResponse(fmt.Errorf("node not found: %s", name))
	}

	deviceStr := getString(input, "boot_device")
	device := parseBootDevice(deviceStr)
	persistent := getBool(input, "persistent")

	d := drv.(bmc.Driver)
	if err := d.SetBootDevice(ctx, device, persistent); err != nil {
		return errorResponse(fmt.Errorf("failed to set boot device: %w", err))
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":          name,
			"boot_device": string(device),
			"persistent":  persistent,
			"status":      "applied",
		}),
	}
}

func handleGetInventory(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err)
	}

	name := getString(input, "id")
	if name == "" {
		name = getString(input, "name")
	}
	if name == "" {
		return errorResponse(fmt.Errorf("id or name required"))
	}

	drv, ok := _drivers.Load(name)
	if !ok {
		return errorResponse(fmt.Errorf("node not found: %s", name))
	}

	d := drv.(bmc.Driver)
	inv, err := d.GetInventory(ctx)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to get inventory: %w", err))
	}

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":               name,
			"manufacturer":     inv.Manufacturer,
			"model":            inv.Model,
			"serial_number":    inv.SerialNumber,
			"firmware_version": inv.FirmwareVersion,
			"cpu_count":        inv.CPUCount,
			"memory_bytes":     inv.MemoryBytes,
			"nic_count":        inv.NICCount,
			"storage_count":    inv.StorageCount,
		}),
	}
}

// Provision prepares the machine for the external image/provisioning service.
// The plugin owns BMC lifecycle only: PXE for the next boot and power on.
func handleProvision(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	d, name, err := driverForRequest(req)
	if err != nil {
		return errorResponse(err)
	}
	if err := d.SetBootDevice(ctx, bmc.BootDevicePxe, false); err != nil {
		return errorResponse(fmt.Errorf("failed to prepare PXE boot: %w", err))
	}
	if err := d.PowerOn(ctx); err != nil {
		return errorResponse(fmt.Errorf("failed to power on for provisioning: %w", err))
	}
	return &plugin.ResourceResponse{Success: true, Result: marshal(map[string]interface{}{
		"id": name, "status": "provisioning", "boot_device": string(bmc.BootDevicePxe),
	})}
}

func handleDeprovision(ctx context.Context, req *plugin.ResourceRequest) *plugin.ResourceResponse {
	d, name, err := driverForRequest(req)
	if err != nil {
		return errorResponse(err)
	}
	if err := d.PowerOff(ctx); err != nil {
		return errorResponse(fmt.Errorf("failed to power off during deprovisioning: %w", err))
	}
	return &plugin.ResourceResponse{Success: true, Result: marshal(map[string]interface{}{
		"id": name, "status": "deprovisioned",
	})}
}

func driverForRequest(req *plugin.ResourceRequest) (bmc.Driver, string, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return nil, "", err
	}
	name := getString(input, "id")
	if name == "" {
		name = getString(input, "name")
	}
	if name == "" {
		return nil, "", fmt.Errorf("id or name required")
	}
	value, ok := _drivers.Load(name)
	if !ok {
		return nil, "", fmt.Errorf("node not found: %s", name)
	}
	d, ok := value.(bmc.Driver)
	if !ok {
		return nil, "", fmt.Errorf("invalid driver for node: %s", name)
	}
	return d, name, nil
}

func doPowerAction(ctx context.Context, req *plugin.ResourceRequest, action func(bmc.Driver) error) *plugin.ResourceResponse {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err)
	}

	name := getString(input, "id")
	if name == "" {
		name = getString(input, "name")
	}
	if name == "" {
		return errorResponse(fmt.Errorf("id or name required"))
	}

	drv, ok := _drivers.Load(name)
	if !ok {
		return errorResponse(fmt.Errorf("node not found: %s", name))
	}

	d := drv.(bmc.Driver)
	if err := action(d); err != nil {
		return errorResponse(fmt.Errorf("power action failed: %w", err))
	}

	powerState, _ := d.GetPowerState(ctx)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":          name,
			"power_state": string(powerState),
			"status":      "applied",
		}),
	}
}

func extractDriverConfig(input map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}

	for _, key := range []string{"address", "username", "password", "interface"} {
		if v, ok := input[key].(string); ok {
			cfg[key] = v
		}
	}

	for _, key := range []string{"insecure", "port"} {
		if v, ok := input[key].(float64); ok {
			cfg[key] = int(v)
		} else if v, ok := input[key].(bool); ok {
			cfg[key] = v
		}
	}

	return cfg
}

func parseBootDevice(s string) bmc.BootDevice {
	switch s {
	case "pxe", "PXE", "network", "Network":
		return bmc.BootDevicePxe
	case "disk", "Disk", "hd", "HDD", "sda":
		return bmc.BootDeviceDisk
	case "cdrom", "CDROM", "cd", "dvd", "DVD":
		return bmc.BootDeviceCdrom
	case "usb", "USB":
		return bmc.BootDeviceUsb
	case "bios", "BIOS", "setup":
		return bmc.BootDeviceBios
	case "none", "None":
		return bmc.BootDeviceNone
	default:
		return bmc.BootDevicePxe
	}
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{
		Success: false,
		Error:   &plugin.Error{Code: "OPERATION_FAILED", Message: err.Error()},
	}
}

func responseErrorMessage(err *plugin.Error) string {
	if err == nil {
		return "operation failed"
	}
	return err.Message
}

func marshal(v map[string]interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
