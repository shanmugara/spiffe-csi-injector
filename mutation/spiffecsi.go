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
	sc.Logger.Info("Mutating pod...", pod.Namespace, pod.Name)

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
	csiVolExists, CsiDriverExists := sc.CheckPodVolume(mpod)
	sc.Logger.Info("csiVolExists", "CsiDriverExists", csiVolExists, CsiDriverExists)
	if !csiVolExists && !CsiDriverExists {
		//sc.Logger.Info("Adding CSI volume to pod", mpod.Name, mpod.Namespace)
		mpod.Spec.Volumes = append(mpod.Spec.Volumes, CSIVolume)
	}

	if csiVolExists && !CsiDriverExists {
		sc.Logger.Info("csiVol exists but CSIdriver not exists")
		var updatedVolumes []corev1.Volume
		//sc.Logger.Debug("Updating CSI volume driver in pod", mpod.Name, mpod.Namespace)
		for _, volume := range mpod.Spec.Volumes {
			if volume.Name != WorkloadSocket {
				updatedVolumes = append(updatedVolumes, volume)
			}
		}
		updatedVolumes = append(updatedVolumes, CSIVolume)
		mpod.Spec.Volumes = updatedVolumes
	} else {
		sc.Logger.Info("DID Not meet the condition csiVolExists && !CsiDriverExists")
	}

	// Define the volume mount
	CSIMount := corev1.VolumeMount{
		Name:      WorkloadSocket,
		MountPath: UdsMountPath,
		ReadOnly:  true,
	}

	//Inject the volume mount to all init-containers
	//sc.Logger.Info("Checking init containers:", CSIVolume, CSIMount)
	if mpod.Spec.InitContainers != nil {
		for i, initContainer := range mpod.Spec.InitContainers {
			//sc.Logger.Info("Checking volume in init container:", initContainer.Name)
			if !sc.CheckContainerVolumeMount(initContainer) {
				//sc.Logger.Info("Injecting volume to init container:", initContainer.Name)
				mpod.Spec.InitContainers[i].VolumeMounts = append(initContainer.VolumeMounts, CSIMount)
			}
		}
	}

	//// DEBUG
	//sc.Logger.Info("***** pre inject pods volumes", mpod.Spec.Volumes)
	//for _, v := range mpod.Spec.Volumes {
	//	if v.Name == WorkloadSocket {
	//		sc.Logger.Info("pre inject validation workload-socket volume name exists:", v.Name)
	//		if v.CSI != nil {
	//			sc.Logger.Info("pre inject validation csi driver name exists:", v.CSI.Driver)
	//		}
	//		if v.EmptyDir != nil {
	//			sc.Logger.Info("pre inject validation empty dir volume name exists:", v.EmptyDir)
	//		}
	//	}
	//}

	//Inject the volume mount to all
	sc.Logger.Info("Injecting volume to containers:", CSIVolume, CSIMount)
	for i, container := range mpod.Spec.Containers {
		sc.Logger.Info("Checking volume in container:", container.Name)
		if !sc.CheckContainerVolumeMount(container) {
			sc.Logger.Info("Injecting volume to container:", container.Name)
			mpod.Spec.Containers[i].VolumeMounts = append(container.VolumeMounts, CSIMount)
		}
	}

	//// DEBUG
	//sc.Logger.Info("***** post pods volumes", mpod.Spec.Volumes)
	//for _, v := range mpod.Spec.Volumes {
	//	if v.Name == WorkloadSocket {
	//		sc.Logger.Info("post validation workload-socket volume name exists:", v.Name)
	//		if v.CSI != nil {
	//			sc.Logger.Info("post validation csi driver name exists:", v.CSI.Driver)
	//		}
	//		if v.EmptyDir != nil {
	//			sc.Logger.Info("post validation empty dir volume name exists:", v.EmptyDir)
	//		}
	//	}
	//}

	return mpod, nil
}

func (sc InjectCSI) CheckPodVolume(pod *corev1.Pod) (bool, bool) {
	sc.Logger.Info("Checking pod volumes:", pod.Namespace, pod.Name)
	VolNameExists := false
	CsiDriverExists := false
	for _, volume := range pod.Spec.Volumes {
		sc.Logger.Info("Checking if volume is workload-socket:", pod.Name)
		if volume.Name == WorkloadSocket {
			sc.Logger.Info("workload-socket volume name exists:", volume.Name)
			VolNameExists = true
		}
		sc.Logger.Info("Checking if volume is CSIDriver:", pod.Name)
		if volume.CSI != nil {
			if volume.CSI.Driver == CsiDriver {
				sc.Logger.Info("CSI driver exists:", volume.Name)
				CsiDriverExists = true
			}
		}
	}
	return VolNameExists, CsiDriverExists
}

func (sc InjectCSI) CheckContainerVolumeMount(container corev1.Container) bool {
	for _, volumeMount := range container.VolumeMounts {
		if volumeMount.Name == WorkloadSocket && volumeMount.MountPath == UdsMountPath {
			return true
		}
	}
	return false
}
