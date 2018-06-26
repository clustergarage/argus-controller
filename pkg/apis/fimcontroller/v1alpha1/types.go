package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FimWatch is a specification for a FimWatch resource
type FimWatch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FimWatchSpec   `json:"spec"`
	Status FimWatchStatus `json:"status"`
}

// FimWatchSpec is the spec for a FimWatch resource
type FimWatchSpec struct {
	DeploymentName string `json:"deploymentName"`
	Replicas       *int32 `json:"replicas"`
}

// FimWatchStatus is the status for a FimWatch resource
type FimWatchStatus struct {
	AvailableReplicas int32 `json:"availableReplicas"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FimWatchList is a list of FimWatch resources
type FimWatchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FimWatch `json:"items"`
}
