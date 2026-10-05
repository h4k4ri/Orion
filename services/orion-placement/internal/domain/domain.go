package domain

type Host struct {
	HostID              string    `json:"host_id"`
	CellID              string    `json:"cell_id"`
	Group               string    `json:"group"`
	Enabled             bool      `json:"enabled"`
	Drained             bool      `json:"drained"`
	NodeAgentURL        string    `json:"node_agent_url,omitempty"`
	VolumeHostAgentURL  string    `json:"volume_host_agent_url,omitempty"`
	NetworkHostAgentURL string    `json:"network_host_agent_url,omitempty"`
	Traits              []string  `json:"traits"`
	Inventory           Inventory `json:"inventory"`
	Generation          int64     `json:"generation"`
	AvailabilityZone    string    `json:"availability_zone,omitempty"`
	Topology            Topology  `json:"topology,omitempty"`
}

// ResourceProvider models Placement's provider tree. A provider may be a
// compute root, NUMA cell, PCI device or any other resource-bearing child.
type ResourceProvider struct {
	UUID             string                       `json:"uuid"`
	Name             string                       `json:"name"`
	ParentProviderID string                       `json:"parent_provider_id,omitempty"`
	RootProviderID   string                       `json:"root_provider_id"`
	Generation       int64                        `json:"generation"`
	Traits           []string                     `json:"traits,omitempty"`
	Inventories      map[string]ProviderInventory `json:"inventories,omitempty"`
}

type ProviderInventory struct {
	Total           int     `json:"total"`
	Reserved        int     `json:"reserved,omitempty"`
	Used            int     `json:"used"`
	AllocationRatio float64 `json:"allocation_ratio,omitempty"`
}

type CreateResourceProviderRequest struct {
	UUID             string                       `json:"uuid,omitempty"`
	Name             string                       `json:"name"`
	ParentProviderID string                       `json:"parent_provider_id,omitempty"`
	Traits           []string                     `json:"traits,omitempty"`
	Inventories      map[string]ProviderInventory `json:"inventories,omitempty"`
}

type UpdateResourceProviderRequest struct {
	UUID        string                       `json:"uuid"`
	Name        string                       `json:"name,omitempty"`
	Traits      []string                     `json:"traits,omitempty"`
	Inventories map[string]ProviderInventory `json:"inventories,omitempty"`
}

type Topology struct {
	Datacenter    string `json:"datacenter,omitempty"`
	Rack          string `json:"rack,omitempty"`
	HostAggregate string `json:"host_aggregate,omitempty"`
}

type NUMANode struct {
	ID                int   `json:"id"`
	VCPUs             []int `json:"vcpus"`
	MemoryMBTotal     int   `json:"memory_mb_total"`
	MemoryAllocatedMB int   `json:"memory_mb_allocated"`
}

type GPUDevice struct {
	ID          string   `json:"id"`
	Vendor      string   `json:"vendor"`
	Model       string   `json:"model"`
	MemoryMB    int      `json:"memory_mb"`
	Traits      []string `json:"traits"`
	AllocatedTo string   `json:"allocated_to,omitempty"`
}

type Inventory struct {
	VCPUsTotal        int         `json:"vcpus_total"`
	VCPUsAllocated    int         `json:"vcpus_allocated"`
	MemoryMBTotal     int         `json:"memory_mb_total"`
	MemoryAllocatedMB int         `json:"memory_mb_allocated"`
	DiskGBTotal       int         `json:"disk_gb_total"`
	DiskAllocatedGB   int         `json:"disk_gb_allocated"`
	NUMA              []NUMANode  `json:"numa"`
	GPUs              []GPUDevice `json:"gpus"`
}

type RegisterHostRequest struct {
	HostID              string      `json:"host_id"`
	CellID              string      `json:"cell_id"`
	Group               string      `json:"group"`
	Enabled             bool        `json:"enabled"`
	Drained             bool        `json:"drained"`
	NodeAgentURL        string      `json:"node_agent_url"`
	VolumeHostAgentURL  string      `json:"volume_host_agent_url"`
	NetworkHostAgentURL string      `json:"network_host_agent_url"`
	Traits              []string    `json:"traits"`
	VCPUs               int         `json:"vcpus_total"`
	MemoryMB            int         `json:"memory_mb_total"`
	DiskGB              int         `json:"disk_gb_total"`
	NUMA                []NUMANode  `json:"numa"`
	GPUs                []GPUDevice `json:"gpus"`
	AvailabilityZone    string      `json:"availability_zone"`
}

type SelectHostRequest struct {
	ServerID               string          `json:"server_id"`
	CellID                 string          `json:"cell_id"`
	VCPUs                  int             `json:"vcpus"`
	MemoryMB               int             `json:"memory_mb"`
	DiskGB                 int             `json:"disk_gb"`
	TraitsRequired         []string        `json:"traits_required"`
	RequireNodeAgent       bool            `json:"require_node_agent"`
	RequireVolumeHostAgent bool            `json:"require_volume_host_agent"`
	AvailabilityZones      []string        `json:"availability_zones,omitempty"`
	AntiAffinityProjectID  string          `json:"anti_affinity_project_id,omitempty"`
	AffinityHostID         string          `json:"affinity_host_id,omitempty"`
	ScoringStrategy        ScoringStrategy `json:"scoring_strategy,omitempty"`
	HostAggregate          string          `json:"host_aggregate,omitempty"`
	ScoreWeights           ScoreWeights    `json:"score_weights,omitempty"`
}

type ScoreWeights struct {
	VCPUs  float64 `json:"vcpus,omitempty"`
	Memory float64 `json:"memory,omitempty"`
	Disk   float64 `json:"disk,omitempty"`
}

type ScoringStrategy string

const (
	ScoringStrategyBinPacking ScoringStrategy = "bin_packing"
	ScoringStrategySpread     ScoringStrategy = "spread"
)

type HostSelection struct {
	CellID string `json:"cell_id"`
	HostID string `json:"host_id"`
}

type ReleaseHostRequest struct {
	ServerID string `json:"server_id"`
	HostID   string `json:"host_id"`
	VCPUs    int    `json:"vcpus"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
}

type UpdateHostStateRequest struct {
	HostID  string `json:"host_id"`
	Enabled *bool  `json:"enabled,omitempty"`
	Drained *bool  `json:"drained,omitempty"`
}

type Reservation struct {
	ID           string `json:"id"`
	HostID       string `json:"host_id"`
	ProjectID    string `json:"project_id"`
	ServerID     string `json:"server_id,omitempty"`
	VCPUs        int    `json:"vcpus"`
	MemoryMB     int    `json:"memory_mb"`
	DiskGB       int    `json:"disk_gb"`
	FencingToken int64  `json:"fencing_token"`
	ExpiresAt    int64  `json:"expires_at"`
	CreatedAt    int64  `json:"created_at"`
}

type CreateReservationRequest struct {
	ID         string `json:"id"`
	HostID     string `json:"host_id"`
	ProjectID  string `json:"project_id"`
	ServerID   string `json:"server_id,omitempty"`
	VCPUs      int    `json:"vcpus"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	TTLSeconds int64  `json:"ttl_seconds"`
}

// AllocationUpdate carries delta values for host inventory allocation.
type AllocationUpdate struct {
	HostID        string
	ExpectedGen   int64
	DeltaVCPUs    int
	DeltaMemoryMB int
	DeltaDiskGB   int
}
