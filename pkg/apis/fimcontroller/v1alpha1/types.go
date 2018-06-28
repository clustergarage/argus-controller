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
	// +optional
	Template corev1.PodTemplateSpec `json:"template"`

	Selector *metav1.LabelSelector `json:"selector" protobuf:"bytes,1,opt,name=selector"`
	Subjects []FimWatcherSubject   `json:"subjects" protobuf:"bytes,2,opt,name=subjects"`
}

// FimWatcherSubject is the spec for a FimWatcherSubject resource
type FimWatcherSubject struct {
	Path   string   `json:"path" protobuf:"bytes,1,opt,name=path"`
	Events []string `json:"events" protobuf:"bytes,2,opt,name=events"`
}

// FimWatcherStatus is the status for a FimWatcher resource
type FimWatcherStatus struct {
	Subjects int32 `json:"subjects"`
	// +optional
	AvailableSubjects int32 `json:"availableSubjects"`
	// +optional
	ReadySubjects int32 `json:"readySubjects"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration"`
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
