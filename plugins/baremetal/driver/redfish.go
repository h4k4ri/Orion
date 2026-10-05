package driver

import (
	"context"
	"fmt"

	"github.com/stmcginnis/gofish"
	"github.com/stmcginnis/gofish/redfish"

	"github.com/horizon/orion/plugins/baremetal/bmc"
)

type redfishDriver struct {
	client *gofish.APIClient
	system *redfish.ComputerSystem
	config RedfishConfig
}

type RedfishConfig struct {
	Address  string
	Username string
	Password string
	Insecure bool
	Port     int
}

func NewRedfishDriver(cfg RedfishConfig) (bmc.Driver, error) {
	protocol := "https"
	if cfg.Insecure {
		protocol = "http"
	}

	host := cfg.Address
	if cfg.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, cfg.Port)
	}

	clientConfig := gofish.ClientConfig{
		Endpoint: fmt.Sprintf("%s://%s", protocol, host),
		Username: cfg.Username,
		Password: cfg.Password,
		Insecure: cfg.Insecure,
	}

	client, err := gofish.Connect(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to redfish: %w", err)
	}

	service := client.Service
	systems, err := service.Systems()
	if err != nil || len(systems) == 0 {
		client.Logout()
		return nil, fmt.Errorf("no systems found")
	}

	return &redfishDriver{
		client: client,
		system: systems[0],
		config: cfg,
	}, nil
}

func (d *redfishDriver) PowerOn(ctx context.Context) error {
	return d.system.Reset(redfish.OnResetType)
}

func (d *redfishDriver) PowerOff(ctx context.Context) error {
	return d.system.Reset(redfish.ForceOffResetType)
}

func (d *redfishDriver) Reboot(ctx context.Context) error {
	return d.system.Reset(redfish.ForceRestartResetType)
}

func (d *redfishDriver) GetPowerState(ctx context.Context) (bmc.PowerState, error) {
	powerState := d.system.PowerState
	switch powerState {
	case redfish.OnPowerState:
		return bmc.PowerStateOn, nil
	case redfish.OffPowerState:
		return bmc.PowerStateOff, nil
	default:
		return bmc.PowerStateUnknown, nil
	}
}

func (d *redfishDriver) SetBootDevice(ctx context.Context, device bmc.BootDevice, persistent bool) error {
	target := mapBootDevice(device)
	enabled := redfish.OnceBootSourceOverrideEnabled
	if persistent {
		enabled = redfish.ContinuousBootSourceOverrideEnabled
	}

	return d.system.SetBoot(redfish.Boot{
		BootSourceOverrideTarget:  target,
		BootSourceOverrideEnabled: enabled,
	})
}

func (d *redfishDriver) GetInventory(ctx context.Context) (*bmc.Inventory, error) {
	inv := &bmc.Inventory{
		Manufacturer: d.system.Manufacturer,
		Model:        d.system.Model,
		SerialNumber: d.system.SerialNumber,
	}

	if d.system.BIOSVersion != "" {
		inv.FirmwareVersion = d.system.BIOSVersion
	}

	processors, _ := d.system.Processors()
	inv.CPUCount = len(processors)

	memory, _ := d.system.Memory()
	var totalMemory int64
	for _, m := range memory {
		if m.CapacityMiB > 0 {
			totalMemory += int64(m.CapacityMiB) * 1024 * 1024
		}
	}
	inv.MemoryBytes = totalMemory

	ethNICs, _ := d.system.EthernetInterfaces()
	inv.NICCount = len(ethNICs)

	storage, _ := d.system.Storage()
	inv.StorageCount = len(storage)

	return inv, nil
}

func (d *redfishDriver) Close(ctx context.Context) error {
	d.client.Logout()
	return nil
}

func mapBootDevice(device bmc.BootDevice) redfish.BootSourceOverrideTarget {
	switch device {
	case bmc.BootDevicePxe:
		return redfish.PxeBootSourceOverrideTarget
	case bmc.BootDeviceDisk:
		return redfish.HddBootSourceOverrideTarget
	case bmc.BootDeviceCdrom:
		return redfish.CdBootSourceOverrideTarget
	case bmc.BootDeviceUsb:
		return redfish.UsbBootSourceOverrideTarget
	case bmc.BootDeviceBios:
		return redfish.BiosSetupBootSourceOverrideTarget
	case bmc.BootDeviceNone:
		return redfish.NoneBootSourceOverrideTarget
	default:
		return redfish.PxeBootSourceOverrideTarget
	}
}
