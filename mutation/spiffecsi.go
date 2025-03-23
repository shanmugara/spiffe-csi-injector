package mutation

import (
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

const (
	WorkloadSocket = "workload-socket"
	UdsMountPath   = "/run/secrets/workload-spiffe-uds"
	CsiDriver      = "csi.spiffe.io"
)

type InjectCSI struct {
	Logger logrus.FieldLogger
}

var _ PodMutator = &InjectCSI{}

func (sc InjectCSI) Name() string {
	return "inject-csi"
}

func (sc InjectCSI) Mutate(pod *corev1.Pod) (*corev1.Pod, error) {
	// Implement the logic to mutate the Pod
	sc.Logger = sc.Logger.WithField("mutate", sc.Name())
	sc.Logger.Info("Mutating pod", pod.Namespace, pod.Name)

	mpod := pod.DeepCopy()
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

	// Add the volume to the pod
	if !sc.CheckPodVolume(mpod) {
		mpod.Spec.Volumes = append(mpod.Spec.Volumes, CSIVolume)
	}

	//Inject the volume mount to all init-containers
	for i, container := range mpod.Spec.InitContainers {
		CSIMount := corev1.VolumeMount{
			Name:      WorkloadSocket,
			MountPath: UdsMountPath,
			ReadOnly:  true,
		}

		if !sc.CheckContainerVolumeMount(container) {
			mpod.Spec.InitContainers[i].VolumeMounts = append(container.VolumeMounts, CSIMount)
		}
	}

	//Inject the volume mount to all containers
	for i, container := range mpod.Spec.Containers {
		CSIMount := corev1.VolumeMount{
			Name:      WorkloadSocket,
			MountPath: UdsMountPath,
			ReadOnly:  true,
		}

		if !sc.CheckContainerVolumeMount(container) {
			mpod.Spec.Containers[i].VolumeMounts = append(container.VolumeMounts, CSIMount)
		}
	}

	return mpod, nil
}

func (sc InjectCSI) CheckPodVolume(pod *corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == WorkloadSocket && volume.CSI.Driver == CsiDriver {
			return true
		}
	}
	return false
}

func (sc InjectCSI) CheckContainerVolumeMount(container corev1.Container) bool {
	for _, volumeMount := range container.VolumeMounts {
		if volumeMount.Name == WorkloadSocket && volumeMount.MountPath == UdsMountPath {
			return true
		}
	}
	return false
}
