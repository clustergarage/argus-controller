package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
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
	Path   string   `json:"path" protobuf:"bytes,1,opt,name=path"`
	Events []string `json:"events" protobuf:"bytes,2,opt,name=events"`
}

// FimWatcherStatus is the status for a FimWatcher resource
type FimWatcherStatus struct {
	AvailableSubjects int32 `json:"availableSubjects"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// FimWatcherList is a list of FimWatcher resources
type FimWatcherList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FimWatcher `json:"items"`
}
