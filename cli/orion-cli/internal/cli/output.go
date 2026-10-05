package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/image"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/libs/go/kit/task"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
)

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printField(v any, field string) error {
	value, err := fieldValue(v, field)
	if err != nil {
		return err
	}
	fmt.Println(value)
	return nil
}

func printFieldList(items any, field string) error {
	payload, err := json.Marshal(items)
	if err != nil {
		return err
	}

	var decoded []any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}

	for _, item := range decoded {
		value, err := fieldValue(item, field)
		if err != nil {
			return err
		}
		fmt.Println(value)
	}

	return nil
}

func printHealthTable(items []HealthResult) {
	printSectionTitle("Health")
	printTable([]string{"SERVICE", "STATUS", "URL", "ERROR"}, toRowsHealth(items))
}

func printTokenHuman(token authn.Token) {
	scope := string(token.Actor.Scope.Type)
	if token.Actor.Scope.ProjectID != "" {
		scope = fmt.Sprintf("%s project=%s", token.Actor.Scope.Type, token.Actor.Scope.ProjectID)
	}

	printBlock("Token", map[string]string{
		"value":      token.Value,
		"user":       fmt.Sprintf("%s (%s)", token.Actor.Username, token.Actor.UserID),
		"scope":      scope,
		"roles":      joinRoles(token.Actor.Roles),
		"expires_at": token.ExpiresAt.Format(time.RFC3339),
	})
}

func printImagesTable(items []image.Image, wide bool) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.ID, item.Name, item.Status, item.DiskFormat, fmt.Sprintf("%d", item.MinDiskGB)}
		if wide {
			row = append(row, item.Architecture, fmt.Sprintf("%d", item.SizeBytes))
		}
		rows = append(rows, row)
	}
	printSectionTitle("Images")
	headers := []string{"ID", "NAME", "STATUS", "FORMAT", "MIN_DISK_GB"}
	if wide {
		headers = append(headers, "ARCH", "SIZE_BYTES")
	}
	printTable(headers, rows)
}

func printHostsTable(items []Host, wide bool) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{
			item.HostID,
			item.CellID,
			item.Group,
			boolString(item.Enabled),
			boolString(item.Drained),
			fmt.Sprintf("%d/%d", item.Inventory.VCPUsAllocated, item.Inventory.VCPUsTotal),
			fmt.Sprintf("%d/%d", item.Inventory.MemoryAllocatedMB, item.Inventory.MemoryMBTotal),
			fmt.Sprintf("%d/%d", item.Inventory.DiskAllocatedGB, item.Inventory.DiskGBTotal),
		}
		if wide {
			row = append(
				row,
				strings.Join(item.Traits, ","),
				fmt.Sprintf("%d", len(item.Inventory.NUMA)),
				fmt.Sprintf("%d", len(item.Inventory.GPUs)),
				fmt.Sprintf("%d", item.Generation),
			)
		}
		rows = append(rows, row)
	}
	printSectionTitle("Hosts")
	headers := []string{"HOST", "CELL", "GROUP", "ENABLED", "DRAINED", "VCPUS", "MEM_MB", "DISK_GB"}
	if wide {
		headers = append(headers, "TRAITS", "NUMA", "GPUS", "GEN")
	}
	printTable(headers, rows)
}

func printNetworksTable(items []networkkit.Network, wide bool) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.ID, item.ProjectID, item.Name, item.Status}
		if wide {
			row = append(row, item.CreatedAt.Format(time.RFC3339))
		}
		rows = append(rows, row)
	}
	printSectionTitle("Networks")
	headers := []string{"ID", "PROJECT", "NAME", "STATUS"}
	if wide {
		headers = append(headers, "CREATED_AT")
	}
	printTable(headers, rows)
}

func printSubnetsTable(items []networkkit.Subnet, wide bool) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.ID, item.NetworkID, item.Name, item.CIDR, item.GatewayIP, boolString(item.EnableDHCP)}
		if wide {
			row = append(row, item.ProjectID, item.DHCPOptionsUUID)
		}
		rows = append(rows, row)
	}
	printSectionTitle("Subnets")
	headers := []string{"ID", "NETWORK", "NAME", "CIDR", "GATEWAY", "DHCP"}
	if wide {
		headers = append(headers, "PROJECT", "DHCP_OPTIONS_UUID")
	}
	printTable(headers, rows)
}

func printPortsTable(items []networkkit.Port, wide bool) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.ID, item.NetworkID, item.DeviceID, item.MACAddress, fixedIP(item), item.Status}
		if wide {
			row = append(row, item.DeviceOwner, item.BindingHostID, item.VIFType)
		}
		rows = append(rows, row)
	}
	printSectionTitle("Ports")
	headers := []string{"ID", "NETWORK", "DEVICE", "MAC", "IP", "STATUS"}
	if wide {
		headers = append(headers, "OWNER", "HOST", "VIF")
	}
	printTable(headers, rows)
}

func printServersTable(items []compute.Server, wide bool) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.ID, item.Name, item.Flavor, item.HostID, item.Status}
		if wide {
			row = append(row, item.CellID, strings.Join(item.PortIDs, ","), strings.Join(item.VolumeIDs, ","))
		}
		rows = append(rows, row)
	}
	printSectionTitle("Servers")
	headers := []string{"ID", "NAME", "FLAVOR", "HOST", "STATUS"}
	if wide {
		headers = append(headers, "CELL", "PORTS", "VOLUMES")
	}
	printTable(headers, rows)
}

func printVolumesTable(items []volumekit.Volume, wide bool) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.ID, item.Name, fmt.Sprintf("%d", item.SizeGB), item.Status, item.AttachedServerID}
		if wide {
			row = append(row, item.HostID, item.CellID, item.BackendType, item.DevicePath)
		}
		rows = append(rows, row)
	}
	printSectionTitle("Volumes")
	headers := []string{"ID", "NAME", "SIZE_GB", "STATUS", "ATTACHED_TO"}
	if wide {
		headers = append(headers, "HOST", "CELL", "BACKEND", "DEVICE_PATH")
	}
	printTable(headers, rows)
}

func printTaskHuman(item task.Task) {
	printBlock("Task", map[string]string{
		"id":            item.ID,
		"kind":          item.Kind,
		"status":        string(item.Status),
		"target_ref":    item.TargetRef,
		"cell_id":       item.CellID,
		"host_id":       item.HostID,
		"requested_by":  item.RequestedBy,
		"request_id":    item.RequestID,
		"error_code":    item.ErrorCode,
		"error_message": item.ErrorMessage,
	})
}

func printServerHuman(item compute.Server) {
	printBlock("Server", map[string]string{
		"id":         item.ID,
		"project_id": item.ProjectID,
		"name":       item.Name,
		"image_id":   item.ImageID,
		"flavor":     item.Flavor,
		"cell_id":    item.CellID,
		"host_id":    item.HostID,
		"status":     item.Status,
		"task_id":    item.TaskID,
		"port_ids":   strings.Join(item.PortIDs, ","),
		"volume_ids": strings.Join(item.VolumeIDs, ","),
	})
}

func printImageHuman(item image.Image) {
	printBlock("Image", map[string]string{
		"id":               item.ID,
		"name":             item.Name,
		"status":           item.Status,
		"disk_format":      item.DiskFormat,
		"container_format": item.ContainerFormat,
		"visibility":       item.Visibility,
		"architecture":     item.Architecture,
		"min_disk_gb":      fmt.Sprintf("%d", item.MinDiskGB),
		"size_bytes":       fmt.Sprintf("%d", item.SizeBytes),
		"checksum_sha256":  item.ChecksumSHA256,
		"path":             item.Path,
	})
}

func printHostHuman(item Host) {
	printBlock("Host", map[string]string{
		"host_id":             item.HostID,
		"cell_id":             item.CellID,
		"group":               item.Group,
		"enabled":             boolString(item.Enabled),
		"drained":             boolString(item.Drained),
		"traits":              strings.Join(item.Traits, ","),
		"vcpus_total":         fmt.Sprintf("%d", item.Inventory.VCPUsTotal),
		"vcpus_allocated":     fmt.Sprintf("%d", item.Inventory.VCPUsAllocated),
		"memory_mb_total":     fmt.Sprintf("%d", item.Inventory.MemoryMBTotal),
		"memory_mb_allocated": fmt.Sprintf("%d", item.Inventory.MemoryAllocatedMB),
		"disk_gb_total":       fmt.Sprintf("%d", item.Inventory.DiskGBTotal),
		"disk_gb_allocated":   fmt.Sprintf("%d", item.Inventory.DiskAllocatedGB),
		"numa":                renderHostNUMA(item.Inventory.NUMA),
		"gpus":                renderHostGPUs(item.Inventory.GPUs),
	})
}

func printNetworkHuman(item networkkit.Network) {
	printBlock("Network", map[string]string{
		"id":         item.ID,
		"project_id": item.ProjectID,
		"name":       item.Name,
		"status":     item.Status,
	})
}

func printSubnetHuman(item networkkit.Subnet) {
	printBlock("Subnet", map[string]string{
		"id":                item.ID,
		"project_id":        item.ProjectID,
		"network_id":        item.NetworkID,
		"name":              item.Name,
		"cidr":              item.CIDR,
		"gateway_ip":        item.GatewayIP,
		"enable_dhcp":       boolString(item.EnableDHCP),
		"dhcp_options_uuid": item.DHCPOptionsUUID,
	})
}

func printPortHuman(item networkkit.Port) {
	printBlock("Port", map[string]string{
		"id":              item.ID,
		"project_id":      item.ProjectID,
		"network_id":      item.NetworkID,
		"device_id":       item.DeviceID,
		"device_owner":    item.DeviceOwner,
		"binding_host_id": item.BindingHostID,
		"mac_address":     item.MACAddress,
		"fixed_ip":        fixedIP(item),
		"status":          item.Status,
		"vif_type":        item.VIFType,
		"vnic_type":       item.VNICType,
	})
}

func printVolumeHuman(item volumekit.Volume) {
	printBlock("Volume", map[string]string{
		"id":                 item.ID,
		"project_id":         item.ProjectID,
		"name":               item.Name,
		"status":             item.Status,
		"size_gb":            fmt.Sprintf("%d", item.SizeGB),
		"cell_id":            item.CellID,
		"host_id":            item.HostID,
		"backend_type":       item.BackendType,
		"device_path":        item.DevicePath,
		"attached_server_id": item.AttachedServerID,
	})
}

func printSectionTitle(title string) {
	fmt.Printf("[%s]\n", strings.ToUpper(title))
}

func printBlock(title string, values map[string]string) {
	printSectionTitle(title)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	rows := make([][]string, 0, len(keys))
	for _, key := range ordered(keys) {
		rows = append(rows, []string{key, values[key]})
	}
	printTable([]string{"FIELD", "VALUE"}, rows)
}

func printTable(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = len(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	printBorder(widths)
	printRow(widths, headers)
	printBorder(widths)
	for _, row := range rows {
		printRow(widths, row)
	}
	printBorder(widths)
}

func printBorder(widths []int) {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		parts = append(parts, strings.Repeat("-", width+2))
	}
	fmt.Printf("+%s+\n", strings.Join(parts, "+"))
}

func printRow(widths []int, row []string) {
	formatted := make([]string, 0, len(row))
	for i, cell := range row {
		formatted = append(formatted, fmt.Sprintf(" %-*s ", widths[i], cell))
	}
	fmt.Printf("|%s|\n", strings.Join(formatted, "|"))
}

func toRowsHealth(items []HealthResult) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.Name, item.Status, item.URL, item.Error})
	}
	return rows
}

func joinRoles(items []authn.Role) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, string(item))
	}
	return strings.Join(values, ",")
}

func ordered(keys []string) []string {
	order := []string{
		"value",
		"user",
		"scope",
		"roles",
		"expires_at",
		"id",
		"project_id",
		"name",
		"status",
		"image_id",
		"flavor",
		"cell_id",
		"host_id",
		"task_id",
		"port_ids",
		"network_id",
		"cidr",
		"gateway_ip",
		"enable_dhcp",
		"dhcp_options_uuid",
		"device_id",
		"device_owner",
		"binding_host_id",
		"mac_address",
		"fixed_ip",
		"vif_type",
		"vnic_type",
		"group",
		"enabled",
		"drained",
		"traits",
		"vcpus_total",
		"vcpus_allocated",
		"memory_mb_total",
		"memory_mb_allocated",
		"disk_gb_total",
		"disk_gb_allocated",
		"numa",
		"gpus",
		"kind",
		"target_ref",
		"requested_by",
		"request_id",
		"error_code",
		"error_message",
		"disk_format",
		"container_format",
		"visibility",
		"architecture",
		"min_disk_gb",
		"size_bytes",
		"checksum_sha256",
		"path",
		"volume_ids",
		"size_gb",
		"backend_type",
		"device_path",
		"attached_server_id",
	}
	rank := map[string]int{}
	for i, item := range order {
		rank[item] = i
	}
	sortFn := func(i, j int) bool {
		left, okLeft := rank[keys[i]]
		right, okRight := rank[keys[j]]
		if okLeft && okRight {
			return left < right
		}
		if okLeft {
			return true
		}
		if okRight {
			return false
		}
		return keys[i] < keys[j]
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if !sortFn(i, j) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func renderHostNUMA(nodes []HostNUMANode) string {
	if len(nodes) == 0 {
		return ""
	}

	items := make([]string, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, fmt.Sprintf("%d:%d/%d", node.ID, node.MemoryAllocatedMB, node.MemoryMBTotal))
	}
	return strings.Join(items, ",")
}

func renderHostGPUs(gpus []HostGPUDevice) string {
	if len(gpus) == 0 {
		return ""
	}

	items := make([]string, 0, len(gpus))
	for _, gpu := range gpus {
		label := strings.TrimSpace(strings.Join([]string{gpu.Vendor, gpu.Model}, " "))
		if label == "" {
			label = gpu.ID
		}
		items = append(items, fmt.Sprintf("%s:%dMB", label, gpu.MemoryMB))
	}
	return strings.Join(items, ",")
}

func fieldValue(v any, field string) (string, error) {
	if strings.TrimSpace(field) == "" {
		return "", errors.New("missing field")
	}

	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}

	var current any
	if err := json.Unmarshal(payload, &current); err != nil {
		return "", err
	}

	for _, part := range strings.Split(field, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("field %q not found", field)
		}
		value, ok := object[part]
		if !ok {
			return "", fmt.Errorf("field %q not found", field)
		}
		current = value
	}

	switch value := current.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case bool:
		return boolString(value), nil
	case float64:
		if value == float64(int64(value)) {
			return fmt.Sprintf("%d", int64(value)), nil
		}
		return fmt.Sprintf("%v", value), nil
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			items = append(items, fmt.Sprintf("%v", item))
		}
		return strings.Join(items, ","), nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func fixedIP(item networkkit.Port) string {
	if len(item.FixedIPs) == 0 {
		return ""
	}
	return item.FixedIPs[0].IPAddress
}
