package mutation

import (
	"context"
	"maps"
	"os"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	WorkloadSocket = "workload-socket"
	UdsMountPath1  = "/var/run/secrets/workload-spiffe-uds"
	UdsMountPath2  = "/run/secrets/workload-spiffe-uds"
	CsiDriver      = "csi.spiffe.io"
	SpiffeEnvVar   = "SPIFFE_ENDPOINT_SOCKET"
	ExtraEnvCmName = "spiffe-csi-injector-extra-env"
)

type InjectCSI struct {
	Logger logrus.FieldLogger
	Client client.Client
}

type ExtraEnv map[string]string

var _ PodMutator = &InjectCSI{}

func (sc InjectCSI) Name() string {
	return "inject-csi"
}

func (sc InjectCSI) Mutate(pod *corev1.Pod) (*corev1.Pod, error) {
	// Implement the logic to mutate the Pod
	sc.Logger = sc.Logger.WithField("mutate", sc.Name())
	sc.Logger.Info("Mutating pod...", pod.Namespace, pod.Name)

	mpod := pod.DeepCopy()
	if err := sc.InjectCsiVolume(mpod); err != nil {
		return nil, err
	}
	if err := sc.InjectVolumeMount(mpod); err != nil {
		return nil, err
	}
	if err := sc.InjectEnv(mpod); err != nil {
		return nil, err
	}

	return mpod, nil
}

// findWorkloadSocketVolume returns the index of the pod's workload-socket
// Volume, if any.
func (sc InjectCSI) findWorkloadSocketVolume(pod *corev1.Pod) (int, bool) {
	for i, volume := range pod.Spec.Volumes {
		if volume.Name == WorkloadSocket {
			return i, true
		}
	}
	return -1, false
}

// csiVolumeIsCorrect reports whether volume already uses the expected CSI
// driver, i.e. no mutation is needed.
func (sc InjectCSI) csiVolumeIsCorrect(volume corev1.Volume) bool {
	return volume.CSI != nil && volume.CSI.Driver == CsiDriver
}

func (sc InjectCSI) CheckContainerVolumeMount(container corev1.Container) (bool, bool) {
	var udsMountPath1Exists, udsMountPath2Exists bool
	for _, volumeMount := range container.VolumeMounts {
		if volumeMount.Name != WorkloadSocket {
			continue
		}
		switch volumeMount.MountPath {
		case UdsMountPath1:
			udsMountPath1Exists = true
		case UdsMountPath2:
			udsMountPath2Exists = true
		}
	}
	return udsMountPath1Exists, udsMountPath2Exists
}

func (sc *InjectCSI) InjectCsiVolume(mpod *corev1.Pod) error {
	var yes = true

	CSIVolume := corev1.Volume{
		Name: WorkloadSocket,
		VolumeSource: corev1.VolumeSource{
			CSI: &corev1.CSIVolumeSource{
				Driver:   CsiDriver,
				ReadOnly: &yes,
			},
		},
	}

	// Ensure a workload-socket Volume exists and uses the expected CSI
	// driver, regardless of whether that driver happens to already be
	// mounted under some other volume name.
	idx, found := sc.findWorkloadSocketVolume(mpod)
	switch {
	case !found:
		sc.Logger.Info("Adding CSI volume to pod", mpod.Name, mpod.Namespace)
		mpod.Spec.Volumes = append(mpod.Spec.Volumes, CSIVolume)
	case !sc.csiVolumeIsCorrect(mpod.Spec.Volumes[idx]):
		sc.Logger.Info("workload-socket volume exists but uses the wrong driver, replacing:", mpod.Name)
		mpod.Spec.Volumes[idx] = CSIVolume
	default:
		sc.Logger.Info("workload-socket volume already correctly configured:", mpod.Name)
	}
	return nil
}

func (sc InjectCSI) InjectVolumeMount(mpod *corev1.Pod) error {
	// Define the volume mount
	CSIMount := corev1.VolumeMount{
		Name:      WorkloadSocket,
		MountPath: UdsMountPath1,
		ReadOnly:  true,
	}

	CSIMount2 := corev1.VolumeMount{
		Name:      WorkloadSocket,
		MountPath: UdsMountPath2,
		ReadOnly:  true,
	}

	//Inject the volume mount to all init-containers
	//sc.Logger.Info("Checking init containers:", CSIVolume, CSIMount)
	if mpod.Spec.InitContainers != nil {
		for i := range mpod.Spec.InitContainers {
			initContainer := &mpod.Spec.InitContainers[i]
			//sc.Logger.Info("Checking volume in init container:", initContainer.Name)
			udsPath1, udsPath2 := sc.CheckContainerVolumeMount(*initContainer)

			if !udsPath1 {
				sc.Logger.Info("Injecting volume path1 to init container:", initContainer.Name)
				initContainer.VolumeMounts = append(initContainer.VolumeMounts, CSIMount)
			}
			if !udsPath2 {
				sc.Logger.Info("Injecting volume path2 to init container:", initContainer.Name)
				initContainer.VolumeMounts = append(initContainer.VolumeMounts, CSIMount2)
			}
		}
	}

	//Inject the volume mount to all
	sc.Logger.Info("Injecting volume to containers")
	for i := range mpod.Spec.Containers {
		container := &mpod.Spec.Containers[i]
		sc.Logger.Info("Checking volume in container:", container.Name)
		udsPath1, udsPath2 := sc.CheckContainerVolumeMount(*container)
		if !udsPath1 {
			sc.Logger.Info("Injecting volume path1 to container:", container.Name)
			container.VolumeMounts = append(container.VolumeMounts, CSIMount)
			sc.Logger.Info("container volume mounts 1:", mpod.Spec.Containers[i].VolumeMounts)
		}
		if !udsPath2 {
			sc.Logger.Info("Injecting volume path2 to container:", container.Name)
			container.VolumeMounts = append(container.VolumeMounts, CSIMount2)
			sc.Logger.Info("container volume mounts 2:", mpod.Spec.Containers[i].VolumeMounts)
		}
		sc.Logger.Info("container volume mounts:", container.VolumeMounts)
	}
	return nil
}

// InjectEnv injects the SPIFFE_ENDPOINT_SOCKET environment variable into all containers and init-containers
func (sc InjectCSI) InjectEnv(mpod *corev1.Pod) error {
	// Get extra environment variables from ConfigMap
	envVars, err := sc.GetExtraEnvCm(mpod)
	if err != nil {
		return err
	}

	// Set the SPIFFE_ENDPOINT_SOCKET environment variable
	envVars[SpiffeEnvVar] = "unix://" + UdsMountPath1 + "/socket"

	// Inject environment variables into init-containers
	for k, v := range envVars {
		sc.Logger.Info("ExtraEnv key:", k, "value:", v)

		if mpod.Spec.InitContainers != nil {
			if err := sc.CheckEnvVar(mpod.Spec.InitContainers, k, v); err != nil {
				return err
			}
		}
		if err := sc.CheckEnvVar(mpod.Spec.Containers, k, v); err != nil {
			return err
		}
	}
	return nil
}

func (sc InjectCSI) CheckEnvVar(containers []corev1.Container, env string, val string) error {
	for i := range containers {
		found := false
		for j, envVar := range containers[i].Env {
			if envVar.Name == env {
				if envVar.Value != val {
					containers[i].Env[j].Value = val
				}
				found = true
				break
			}
		}
		if !found {
			containers[i].Env = append(containers[i].Env, corev1.EnvVar{
				Name:  env,
				Value: val,
			})
		}
	}
	return nil
}

func (sc InjectCSI) GetExtraEnvCm(pod *corev1.Pod) (ExtraEnv, error) {
	ctx := context.Background()

	extraEnv := make(ExtraEnv)

	cm := &corev1.ConfigMap{}
	err := sc.Client.Get(ctx, client.ObjectKey{Name: ExtraEnvCmName, Namespace: os.Getenv("POD_NAMESPACE")}, cm)
	if apierrors.IsNotFound(err) {
		sc.Logger.Info("ConfigMap", ExtraEnvCmName, "not found in namespace", os.Getenv("POD_NAMESPACE"))
		return extraEnv, nil
	} else if err != nil {
		sc.Logger.Error("Error getting ConfigMap", ExtraEnvCmName, ":", err)
		return extraEnv, err
	}

	if len(cm.Data) > 0 {
		maps.Copy(extraEnv, cm.Data)

	} else {
		sc.Logger.Info("ConfigMap", ExtraEnvCmName, "has no data")
	}

	return extraEnv, nil

}
