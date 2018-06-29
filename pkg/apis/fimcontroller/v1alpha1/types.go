package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
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
	Selector *metav1.LabelSelector `json:"selector" protobuf:"bytes,1,opt,name=selector"`
	Subjects []FimWatcherSubject   `json:"subjects" protobuf:"bytes,2,opt,name=subjects"`
}

// FimWatcherSubject is the spec for a FimWatcherSubject resource
type FimWatcherSubject struct {
	Paths  []string `json:"paths" protobuf:"bytes,1,opt,name=paths"`
	Events []string `json:"events" protobuf:"bytes,2,opt,name=events"`
}

// FimWatcherStatus is the status for a FimWatcher resource
type FimWatcherStatus struct {
	ObservablePods string `json:"observablePods"`

	// +optional
	Conditions []FimWatcherCondition `json:"conditions"`
}

type FimWatcherConditionType string

const (
	// FimWatcherSubjectFailure is added in a fim watcher when one of its pods fails to be created
	// due to insufficient quota, limit ranges, pod security policy, node selectors, etc. or deleted
	// due to kubelet being down or finalizers are failing.
	FimWatcherSubjectFailure = "SubjectFailure"
)

// FimWatcherCondition describes the state of a replica set at a certain point.
type FimWatcherCondition struct {
	Type   FimWatcherConditionType
	Status corev1.ConditionStatus
	// +optional
	LastTransitionTime metav1.Time
	// +optional
	Reason string
	// +optional
	Message string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FimWatcherList is a list of FimWatcher resources
type FimWatcherList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FimWatcher `json:"items"`
}
