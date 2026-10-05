package resources

const (
	StorageVolumeKind    = "orion.io/storage.volume"
	StorageSnapshotKind  = "orion.io/storage.snapshot"
	StorageFilesystemKind = "orion.io/storage.filesystem"
	StorageObjectKind   = "orion.io/storage.object"
	StorageShareKind    = "orion.io/storage.share"

	ComputeInstanceKind = "orion.io/compute.instance"
	ComputeHostKind    = "orion.io/compute.host"
	ComputeBaremetalKind = "orion.io/compute.baremetal"

	NetworkNetworkKind = "orion.io/network.network"
	NetworkSubnetKind = "orion.io/network.subnet"
	NetworkPortKind   = "orion.io/network.port"
	NetworkRouterKind = "orion.io/network.router"
	NetworkFloatingIpKind = "orion.io/network.floatingIp"

	LoadbalancerLoadbalancerKind = "orion.io/loadbalancer.loadbalancer"
	LoadbalancerListenerKind     = "orion.io/loadbalancer.listener"
	LoadbalancerPoolKind         = "orion.io/loadbalancer.pool"
	LoadbalancerMemberKind       = "orion.io/loadbalancer.member"
	LoadbalancerHealthmonitorKind = "orion.io/loadbalancer.healthmonitor"

	SecretSecretKind      = "orion.io/secret.secret"
	SecretKeyKind        = "orion.io/secret.key"
	SecretCertificateKind = "orion.io/secret.certificate"

	DNSZoneKind  = "orion.io/dns.zone"
	DNSRecordKind = "orion.io/dns.record"

	ImageImageKind   = "orion.io/image.image"
	BackupJobKind    = "orion.io/backup.job"
	DatabaseInstanceKind = "orion.io/database.instance"

	MonitoringMetricKind = "orion.io/monitoring.metric"
	MonitoringAlarmKind  = "orion.io/monitoring.alarm"

	HAProtectionKind = "orion.io/ha.protection"
)
