package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FimWatcher is a specification for a FimWatcher resource
type FimWatcher struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FimWatcherSpec   `json:"spec"`
	Status FimWatcherStatus `json:"status"`
}

// FimWatcherSpec is the spec for a FimWatcher resource
type FimWatcherSpec struct {
	DeploymentName string `json:"deploymentName"`
	Replicas       *int32 `json:"replicas"`
}

// FimWatcherStatus is the status for a FimWatcher resource
type FimWatcherStatus struct {
	AvailableReplicas int32 `json:"availableReplicas"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FimWatcherList is a list of FimWatcher resources
type FimWatcherList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FimWatcher `json:"items"`
}
