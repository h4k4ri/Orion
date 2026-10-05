package domain

import (
	"time"
)

type PluginDeployment struct {
	ID            string
	Name          string
	PluginID      string
	Replicas      int
	Nodes         []string
	Selector      map[string]string
	Strategy      DeploymentStrategy
	Template      PodTemplate
	Status        DeploymentStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type DeploymentStrategy struct {
	Type          string
	RollingUpdate *RollingUpdateStrategy
}

type RollingUpdateStrategy struct {
	MaxSurge       int
	MaxUnavailable int
}

type PodTemplate struct {
	Metadata PodMetadata
	Spec     PodSpec
}

type PodMetadata struct {
	Labels      map[string]string
	Annotations map[string]string
}

type PodSpec struct {
	NodeSelector map[string]string
	Affinity     map[string]interface{}
	Tolerations  []Toleration
	Containers   []ContainerSpec
	Volumes      []VolumeSpec
}

type Toleration struct {
	Key      string
	Operator string
	Value    string
	Effect   string
}

type ContainerSpec struct {
	Name            string
	Image           string
	Command         []string
	Args            []string
	Env             []EnvVar
	Resources       ResourceRequirements
	Ports           []ContainerPort
	VolumeMounts    []VolumeMount
	LivenessProbe   *Probe
	ReadinessProbe  *Probe
}

type EnvVar struct {
	Name      string
	Value     string
	ValueFrom *EnvVarSource
}

type EnvVarSource struct {
	SecretKeyRef *SecretKeySelector
	ConfigMapRef *ConfigMapKeySelector
}

type SecretKeySelector struct {
	Name string
	Key  string
}

type ConfigMapKeySelector struct {
	Name string
	Key  string
}

type ResourceRequirements struct {
	Limits   ResourceList
	Requests ResourceList
}

type ResourceList struct {
	CPU    string
	Memory string
	Storage string
}

type ContainerPort struct {
	Name          string
	ContainerPort int
	Protocol      string
}

type VolumeMount struct {
	Name      string
	MountPath string
	ReadOnly  bool
}

type VolumeSpec struct {
	Name     string
	Type     string
	Path     string
	Secret   *SecretVolumeSource
	ConfigMap *ConfigMapVolumeSource
}

type SecretVolumeSource struct {
	SecretName string
}

type ConfigMapVolumeSource struct {
	Name string
}

type Probe struct {
	InitialDelaySeconds int
	PeriodSeconds       int
	TimeoutSeconds      int
	FailureThreshold    int
	SuccessThreshold    int
	Handler             ProbeHandler
}

type ProbeHandler struct {
	Exec      *ExecAction
	HTTPGet   *HTTPGetAction
	TCPSocket *TCPSocketAction
}

type ExecAction struct {
	Command []string
}

type HTTPGetAction struct {
	Path   string
	Port   int
	Scheme string
}

type TCPSocketAction struct {
	Port int
}

type DeploymentStatus struct {
	Replicas           int
	ReadyReplicas      int
	AvailableReplicas  int
	UnavailableReplicas int
	UpdatedReplicas    int
	Conditions         []DeploymentCondition
}

type DeploymentCondition struct {
	Type           string
	Status         string
	LastUpdateTime time.Time
	Reason         string
	Message        string
}

type DaemonSet struct {
	ID            string
	Name          string
	PluginID      string
	Nodes         []string
	Selector      map[string]string
	Template      PodTemplate
	Status        DaemonSetStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type DaemonSetStatus struct {
	DesiredNumberScheduled int
	NumberReady            int
	NumberAvailable        int
	NumberUnavailable      int
}

type StatefulSet struct {
	ID            string
	Name          string
	PluginID      string
	Replicas      int
	ServiceName   string
	Selector      map[string]string
	Template      PodTemplate
	VolumeClaimTemplates []PersistentVolumeClaim
	Status        StatefulSetStatus
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PersistentVolumeClaim struct {
	Name        string
	StorageClass string
	AccessMode  string
	Capacity    string
}

type StatefulSetStatus struct {
	Replicas       int
	ReadyReplicas   int
	CurrentReplicas int
	UpdatedReplicas int
}
