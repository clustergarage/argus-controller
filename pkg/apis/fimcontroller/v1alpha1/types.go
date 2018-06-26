package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FimListener is a specification for a FimListener resource
type FimListener struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FimListenerSpec   `json:"spec"`
	Status FimListenerStatus `json:"status"`
}

// FimListenerSpec is the spec for a FimListener resource
type FimListenerSpec struct {
	DeploymentName string `json:"deploymentName"`
	Replicas       *int32 `json:"replicas"`
}

// FimListenerStatus is the status for a FimListener resource
type FimListenerStatus struct {
	AvailableReplicas int32 `json:"availableReplicas"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FimListenerList is a list of FimListener resources
type FimListenerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FimListener `json:"items"`
}
