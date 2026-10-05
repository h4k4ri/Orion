package bmc

import "context"

type PowerState string

const (
	PowerStateOn       PowerState = "on"
	PowerStateOff      PowerState = "off"
	PowerStateUnknown  PowerState = "unknown"
)

type BootDevice string

const (
	BootDevicePxe     BootDevice = "pxe"
	BootDeviceDisk    BootDevice = "disk"
	BootDeviceCdrom   BootDevice = "cdrom"
	BootDeviceUsb     BootDevice = "usb"
	BootDeviceBios    BootDevice = "bios"
	BootDeviceNone    BootDevice = "none"
)

type Inventory struct {
	Manufacturer  string
	Model         string
	SerialNumber  string
	FirmwareVersion string
	CPUCount      int
	MemoryBytes   int64
	NICCount      int
	StorageCount  int
}

type Driver interface {
	PowerOn(ctx context.Context) error
	PowerOff(ctx context.Context) error
	Reboot(ctx context.Context) error
	GetPowerState(ctx context.Context) (PowerState, error)
	SetBootDevice(ctx context.Context, device BootDevice, persistent bool) error
	GetInventory(ctx context.Context) (*Inventory, error)
	Close(ctx context.Context) error
}
