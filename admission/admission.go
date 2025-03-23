package admission

import (
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"spiffe-csi-injector/mutation"
)

const (
	ManagedCSILabel = "omega.k8s.io/managed-csi"
)

type Admitter struct {
	Logger  *logrus.Entry
	Request *admissionv1.AdmissionRequest
}

func (a *Admitter) MutatePodReview() (*admissionv1.AdmissionReview, error) {
	// Implement the logic to mutate the PodReview
	pod, err := a.Pod() //Check of the object in the request is a Pod

	if err != nil {
		e := fmt.Sprintf("failed to get Pod from the request: %v", err)
		return reviewResponse(a.Request.UID, false, http.StatusBadRequest, e), err
	}

	// Check if the Pod is managed by the CSI driver webhook

	if pod.Labels[ManagedCSILabel] != "true" {
		a.Logger.Info("Pod is not managed by the CSI driver", pod.Name, pod.Namespace)
		return reviewResponse(a.Request.UID, true, http.StatusOK, "Pod is not managed by the CSI driver"), nil
	}

	//Create a new mutator
	m := mutation.NewMutator(a.Logger)
	patch, err := m.MutatePodPatch(pod)
	if err != nil {
		e := fmt.Sprintf("failed to mutate the Pod: %v", err)
		return reviewResponse(a.Request.UID, false, http.StatusBadRequest, e), err
	}

	return patchReviewResponse(a.Request.UID, patch)
}

func (a *Admitter) Pod() (*corev1.Pod, error) {
	// Check if the object in the request is a Pod
	if a.Request.Kind.Kind != "Pod" {
		a.Logger.Error("The object in the request is not a Pod")
		return nil, fmt.Errorf("object in the request is not a Pod")
	}

	pod := corev1.Pod{}
	if err := json.Unmarshal(a.Request.Object.Raw, &pod); err != nil {
		a.Logger.Error("Failed to unmarshal the Pod object")
		return nil, err
	}

	return &pod, nil
}

func reviewResponse(uid types.UID, allowed bool, httpCode int32, reason string) *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     uid,
			Allowed: allowed,
			Result: &metav1.Status{
				Code:    httpCode,
				Message: reason,
			},
		},
	}
}

func patchReviewResponse(uid types.UID, patch []byte) (*admissionv1.AdmissionReview, error) {
	patchType := admissionv1.PatchTypeJSONPatch

	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:       uid,
			Allowed:   true,
			PatchType: &patchType,
			Patch:     patch,
		},
	}, nil
}
