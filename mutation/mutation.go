package mutation

import (
	"encoding/json"

	"github.com/sirupsen/logrus"
	"github.com/wI2L/jsondiff"
	corev1 "k8s.io/api/core/v1"
)

type Mutator struct {
	Logger *logrus.Entry
}

func NewMutator(logger *logrus.Entry) *Mutator {
	return &Mutator{Logger: logger}
}

type PodMutator interface {
	Mutate(*corev1.Pod) (*corev1.Pod, error)
	Name() string
}

func (m *Mutator) MutatePodPatch(pod *corev1.Pod) ([]byte, error) {
	// Implement the logic to mutate the Pod
	var podName string
	if pod.ObjectMeta.Name != "" {
		podName = pod.ObjectMeta.Name
	} else {
		if pod.ObjectMeta.GenerateName != "" {
			podName = pod.ObjectMeta.GenerateName
		}
	}
	log := logrus.WithField("pod", podName)

	mutations := []PodMutator{
		InjectCSI{Logger: log},
	}
	mpod := pod.DeepCopy()

	for _, mutation := range mutations {
		var err error
		mpod, err = mutation.Mutate(mpod)
		if err != nil {
			return nil, err
		}
	}

	//Create the patch diff
	patch, err := jsondiff.Compare(pod, mpod)
	if err != nil {
		return nil, err
	}

	//Patch bytes
	patchb, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	// return patch bytes
	return patchb, nil
}
