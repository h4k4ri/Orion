package image

type Image struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	DiskFormat      string `json:"disk_format"`
	ContainerFormat string `json:"container_format"`
	Visibility      string `json:"visibility"`
	Architecture    string `json:"architecture"`
	MinDiskGB       int    `json:"min_disk_gb"`
	SizeBytes       int64  `json:"size_bytes"`
	ChecksumSHA256  string `json:"checksum_sha256"`
	Path            string `json:"path"`
}
