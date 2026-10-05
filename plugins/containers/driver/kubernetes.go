package driver

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/horizon/orion/plugins/containers/container"
)

type KubernetesDriver struct {
	client    *kubernetes.Clientset
	namespace string
}

func NewKubernetesDriver(cfg Config) (Driver, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = "/etc/orion/kubernetes.conf"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	namespace := os.Getenv("KUBERNETES_NAMESPACE")
	if namespace == "" {
		namespace = "default"
	}

	return &KubernetesDriver{
		client:    client,
		namespace: namespace,
	}, nil
}

func (d *KubernetesDriver) ListContainers(ctx context.Context, opts ListOptions) ([]container.Container, error) {
	namespace := d.namespace
	if opts.Namespace != "" {
		namespace = opts.Namespace
	}

	pods, err := d.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelsToSelector(opts.Labels),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var containers []container.Container
	for _, pod := range pods.Items {
		for _, c := range pod.Status.ContainerStatuses {
			containers = append(containers, container.Container{
				ID:           string(pod.UID) + "/" + c.Name,
				Name:         c.Name,
				Image:        c.Image,
				State:        containerStateFromK8s(c.State),
				PodID:        string(pod.UID),
				NodeName:     pod.Spec.NodeName,
				Labels:       pod.Labels,
				RestartCount: int(c.RestartCount),
				CreatedAt:    pod.CreationTimestamp.Time,
			})
		}
	}

	return containers, nil
}

func (d *KubernetesDriver) GetContainer(ctx context.Context, id string) (*container.Container, error) {
	podID, containerName := splitID(id)

	pod, err := d.client.CoreV1().Pods(podID).Get(ctx, containerName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	for _, c := range pod.Spec.Containers {
		return &container.Container{
			ID:        id,
			Name:      c.Name,
			Image:     c.Image,
			Command:   c.Command,
			Args:      c.Args,
			Env:       envVarsToContainer(c.Env),
			Ports:     portsToContainer(c.Ports),
			Mounts:    mountsToContainer(c.VolumeMounts),
			Labels:    pod.Labels,
			State:     container.ContainerStateUnknown,
			PodID:     string(pod.UID),
			NodeName:  pod.Spec.NodeName,
			CreatedAt: pod.CreationTimestamp.Time,
		}, nil
	}

	return nil, fmt.Errorf("container not found: %s", id)
}

func (d *KubernetesDriver) CreateContainer(ctx context.Context, input *container.CreateContainerInput) (*container.Container, error) {
	namespace := d.namespace
	if input.Namespace != "" {
		namespace = input.Namespace
	}

	podSpec := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      input.Name,
			Namespace: namespace,
			Labels:    input.Labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:         input.Name,
				Image:        input.Image,
				Command:      input.Command,
				Args:         input.Args,
				Env:          containerEnvToK8s(input.Env),
				Ports:        containerPortsToK8s(input.Ports),
				VolumeMounts: containerMountsToK8s(input.Mounts),
			}},
			NodeName: input.PodID,
		},
	}

	pod, err := d.client.CoreV1().Pods(namespace).Create(ctx, podSpec, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pod: %w", err)
	}

	return &container.Container{
		ID:        string(pod.UID) + "/" + input.Name,
		Name:      input.Name,
		Image:     input.Image,
		Command:   input.Command,
		Args:      input.Args,
		Env:       input.Env,
		Ports:     input.Ports,
		Mounts:    input.Mounts,
		Labels:    input.Labels,
		State:     container.ContainerStateCreated,
		PodID:     string(pod.UID),
		NodeName:  pod.Spec.NodeName,
		CreatedAt: pod.CreationTimestamp.Time,
	}, nil
}

func (d *KubernetesDriver) StartContainer(ctx context.Context, id string) error {
	podID, containerName := splitID(id)
	return d.client.CoreV1().Pods(podID).Bind(ctx, &corev1.Binding{
		ObjectMeta: metav1.ObjectMeta{Name: containerName, Namespace: podID},
		Target:     corev1.ObjectReference{Name: containerName},
	}, metav1.CreateOptions{})
}

func (d *KubernetesDriver) StopContainer(ctx context.Context, id string, timeout int) error {
	podID, containerName := splitID(id)
	gracePeriod := int64(timeout)
	return d.client.CoreV1().Pods(podID).Delete(ctx, containerName, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})
}

func (d *KubernetesDriver) DeleteContainer(ctx context.Context, id string) error {
	podID, containerName := splitID(id)
	return d.client.CoreV1().Pods(podID).Delete(ctx, containerName, metav1.DeleteOptions{})
}

func (d *KubernetesDriver) LogsContainer(ctx context.Context, id string, opts LogsOptions) (string, error) {
	podID, containerName := splitID(id)

	tailLimit := int64(opts.Tail)
	if opts.Tail == 0 {
		tailLimit = 100
	}

	logs, err := d.client.CoreV1().Pods(podID).GetLogs(containerName, &corev1.PodLogOptions{
		Container:  containerName,
		TailLines:  &tailLimit,
		Timestamps: false,
	}).Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(logs)
	return buf.String(), nil
}

func (d *KubernetesDriver) ExecContainer(ctx context.Context, id string, cmd []string) (string, error) {
	podID, containerName := splitID(id)

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = "/etc/orion/kubernetes.conf"
	}

	args := []string{"kubectl", "--kubeconfig", kubeconfig, "exec", "-n", podID, "-it", containerName, "--"}
	args = append(args, cmd...)

	execCmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	err := execCmd.Run()
	if err != nil {
		return "", fmt.Errorf("exec failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

func (d *KubernetesDriver) ListImages(ctx context.Context, opts ListOptions) ([]container.Image, error) {
	return nil, fmt.Errorf("use kubectl to list images")
}

func (d *KubernetesDriver) PullImage(ctx context.Context, ref string) error {
	cmd := exec.CommandContext(ctx, "crictl", "pull", ref)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crictl pull failed: %w, output: %s", err, output)
	}
	return nil
}

func (d *KubernetesDriver) DeleteImage(ctx context.Context, ref string) error {
	cmd := exec.CommandContext(ctx, "crictl", "rmi", ref)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crictl rmi failed: %w, output: %s", err, output)
	}
	return nil
}

func (d *KubernetesDriver) Close() error {
	return nil
}

func splitID(id string) (string, string) {
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '/' {
			return id[:i], id[i+1:]
		}
	}
	return "default", id
}

func containerStateFromK8s(state corev1.ContainerState) container.ContainerState {
	switch {
	case state.Running != nil:
		return container.ContainerStateRunning
	case state.Terminated != nil:
		return container.ContainerStateStopped
	case state.Waiting != nil:
		return container.ContainerStateUnknown
	default:
		return container.ContainerStateUnknown
	}
}

func envVarsToContainer(envs []corev1.EnvVar) []container.EnvVar {
	var result []container.EnvVar
	for _, e := range envs {
		result = append(result, container.EnvVar{Name: e.Name, Value: e.Value})
	}
	return result
}

func containerEnvToK8s(envs []container.EnvVar) []corev1.EnvVar {
	var result []corev1.EnvVar
	for _, e := range envs {
		result = append(result, corev1.EnvVar{Name: e.Name, Value: e.Value})
	}
	return result
}

func portsToContainer(ports []corev1.ContainerPort) []container.PortMapping {
	var result []container.PortMapping
	for _, p := range ports {
		result = append(result, container.PortMapping{
			ContainerPort: int(p.ContainerPort),
			HostPort:      int(p.HostPort),
			Protocol:      string(p.Protocol),
			HostIP:        p.HostIP,
		})
	}
	return result
}

func containerPortsToK8s(ports []container.PortMapping) []corev1.ContainerPort {
	var result []corev1.ContainerPort
	for _, p := range ports {
		result = append(result, corev1.ContainerPort{
			ContainerPort: int32(p.ContainerPort),
			HostPort:      int32(p.HostPort),
			Protocol:      corev1.Protocol(p.Protocol),
			HostIP:        p.HostIP,
		})
	}
	return result
}

func mountsToContainer(mounts []corev1.VolumeMount) []container.Mount {
	var result []container.Mount
	for _, m := range mounts {
		result = append(result, container.Mount{
			Source:      m.Name,
			Destination: m.MountPath,
			ReadOnly:    m.ReadOnly,
		})
	}
	return result
}

func containerMountsToK8s(mounts []container.Mount) []corev1.VolumeMount {
	var result []corev1.VolumeMount
	for _, m := range mounts {
		result = append(result, corev1.VolumeMount{
			Name:      m.Source,
			MountPath: m.Destination,
			ReadOnly:  m.ReadOnly,
		})
	}
	return result
}

func labelsToSelector(labels map[string]string) string {
	var parts []string
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return ""
}
