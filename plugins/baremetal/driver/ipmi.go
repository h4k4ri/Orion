package driver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	ipmi "github.com/bougou/go-ipmi"

	"github.com/horizon/orion/plugins/baremetal/bmc"
)

// ipmiDriver is a long-lived IPMI session. The session is protected because a
// BMC connection is stateful and the plugin may receive concurrent requests.
type ipmiDriver struct {
	client *ipmi.Client
	mu     sync.Mutex
}

type IPMIConfig struct {
	Address   string
	Username  string
	Password  string
	Interface string
	Port      int
}

func NewIPMIDriver(cfg IPMIConfig) (bmc.Driver, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		return nil, fmt.Errorf("IPMI address is required")
	}
	port := cfg.Port
	if port == 0 {
		port = 623
	}
	iface := cfg.Interface
	if iface == "" {
		iface = string(ipmi.InterfaceLanplus)
	}

	client, err := ipmi.NewClient(cfg.Address, port, cfg.Username, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPMI client: %w", err)
	}
	client.WithInterface(ipmi.Interface(iface))
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to IPMI BMC %s:%s: %w", cfg.Address, strconv.Itoa(port), err)
	}
	return &ipmiDriver{client: client}, nil
}

func (d *ipmiDriver) PowerOn(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return d.control(ipmi.ChassisControlPowerUp)
}

func (d *ipmiDriver) PowerOff(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return d.control(ipmi.ChassisControlPowerDown)
}

func (d *ipmiDriver) Reboot(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	return d.control(ipmi.ChassisControlPowerCycle)
}

func (d *ipmiDriver) GetPowerState(ctx context.Context) (bmc.PowerState, error) {
	if err := contextErr(ctx); err != nil {
		return bmc.PowerStateUnknown, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	status, err := d.client.GetChassisStatus()
	if err != nil {
		return bmc.PowerStateUnknown, fmt.Errorf("failed to get chassis status: %w", err)
	}
	if status.PowerIsOn {
		return bmc.PowerStateOn, nil
	}
	return bmc.PowerStateOff, nil
}

func (d *ipmiDriver) SetBootDevice(ctx context.Context, device bmc.BootDevice, persistent bool) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	selector, ok := mapIPMIBootDevice(device)
	if !ok {
		return fmt.Errorf("unsupported IPMI boot device %q", device)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.client.SetBootDevice(selector, ipmi.BIOSBootTypeLegacy, persistent); err != nil {
		return fmt.Errorf("failed to set IPMI boot device: %w", err)
	}
	return nil
}

func (d *ipmiDriver) GetInventory(ctx context.Context) (*bmc.Inventory, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	inv := &bmc.Inventory{}
	if device, err := d.client.GetDeviceID(); err == nil {
		inv.FirmwareVersion = fmt.Sprintf("%d.%02d", device.MajorFirmwareRevision, device.MinorFirmwareRevision)
	}
	frus, err := d.client.GetFRUs()
	if err == nil {
		for _, fru := range frus {
			if fru == nil || !fru.Present() || fru.ProductInfoArea == nil {
				continue
			}
			inv.Manufacturer = strings.TrimSpace(string(fru.ProductInfoArea.Manufacturer))
			inv.Model = strings.TrimSpace(string(fru.ProductInfoArea.Name))
			inv.SerialNumber = strings.TrimSpace(string(fru.ProductInfoArea.SerialNumber))
			break
		}
	}
	return inv, nil
}

func (d *ipmiDriver) Close(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == nil {
		return nil
	}
	return d.client.Close()
}

func (d *ipmiDriver) control(action ipmi.ChassisControl) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.client.ChassisControl(action); err != nil {
		return fmt.Errorf("failed to set IPMI power state: %w", err)
	}
	return nil
}

func contextErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func mapIPMIBootDevice(device bmc.BootDevice) (ipmi.BootDeviceSelector, bool) {
	switch device {
	case bmc.BootDevicePxe:
		return ipmi.BootDeviceSelectorForcePXE, true
	case bmc.BootDeviceDisk, bmc.BootDeviceUsb:
		return ipmi.BootDeviceSelectorForceHardDrive, true
	case bmc.BootDeviceCdrom:
		return ipmi.BootDeviceSelectorForceCDROM, true
	case bmc.BootDeviceBios:
		return ipmi.BootDeviceSelectorForceBIOSSetup, true
	case bmc.BootDeviceNone:
		return ipmi.BootDeviceSelectorNoOverride, true
	default:
		return 0, false
	}
}
