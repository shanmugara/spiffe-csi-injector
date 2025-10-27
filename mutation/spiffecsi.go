package mutation

import (
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

const (
	WorkloadSocket = "workload-socket"
	UdsMountPath1  = "/var/run/secrets/workload-spiffe-uds"
	UdsMountPath2  = "/run/secrets/workload-spiffe-uds"
	CsiDriver      = "csi.spiffe.io"
	SpiffeEnvVar   = "SPIFFE_ENDPOINT_SOCKET"
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

// CheckPodVolume checks if the pod has the volume and csi driver
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

	// Add the volume to the pod
	csiVolExists, CsiDriverExists := sc.CheckPodVolume(mpod)
	sc.Logger.Info("csiVolExists:", csiVolExists, "CsiDriverExists:", CsiDriverExists)
	if !csiVolExists && !CsiDriverExists {
		//sc.Logger.Info("Adding CSI volume to pod", mpod.Name, mpod.Namespace)
		mpod.Spec.Volumes = append(mpod.Spec.Volumes, CSIVolume)
	}

	if csiVolExists && !CsiDriverExists {
		sc.Logger.Info("csiVol exists but CSIdriver does not exist")
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

func (sc InjectCSI) InjectEnv(mpod *corev1.Pod) error {
	if mpod.Spec.InitContainers != nil {
		if err := sc.CheckEnvVar(mpod.Spec.InitContainers); err != nil {
			return err
		}
	}
	if err := sc.CheckEnvVar(mpod.Spec.Containers); err != nil {
		return err
	}
	return nil
}

func (sc InjectCSI) CheckEnvVar(containers []corev1.Container) error {
	EnvValue := "unix://" + UdsMountPath1 + "/socket"
	for i := range containers {
		if containers[i].Env == nil {
			containers[i].Env = []corev1.EnvVar{
				{
					Name:  SpiffeEnvVar,
					Value: EnvValue,
				},
			}
		} else {
			for j, envVar := range containers[i].Env {
				if envVar.Name == SpiffeEnvVar {
					if envVar.Value != EnvValue {
						containers[i].Env[j].Value = EnvValue
					}
					break
				}
			}
			containers[i].Env = append(containers[i].Env, corev1.EnvVar{
				Name:  SpiffeEnvVar,
				Value: EnvValue,
			})
		}
	}
	return nil
}
