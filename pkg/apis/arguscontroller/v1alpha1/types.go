package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ArgusWatcher is a specification for a ArgusWatcher resource.
type ArgusWatcher struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ArgusWatcherSpec   `json:"spec"`
	Status ArgusWatcherStatus `json:"status"`
}

// ArgusWatcherSpec is the spec for a ArgusWatcher resource.
type ArgusWatcherSpec struct {
	Selector  *metav1.LabelSelector  `json:"selector" protobuf:"bytes,1,req,name=selector"`
	Subjects  []*ArgusWatcherSubject `json:"subjects" protobuf:"bytes,2,rep,name=subjects"`
	LogFormat string                 `json:"logFormat,omitempty" protobuf:"bytes,3,opt,name=logFormat"`
}

// ArgusWatcherSubject is the spec for a ArgusWatcherSubject resource.
type ArgusWatcherSubject struct {
	Paths      []string          `json:"paths" protobuf:"bytes,1,rep,name=paths"`
	Events     []string          `json:"events" protobuf:"bytes,2,rep,name=events"`
	Ignore     []string          `json:"ignore,omitempty" protobuf:"bytes,3,rep,name=ignore"`
	OnlyDir    bool              `json:"onlyDir,omitempty" protobuf:"bytes,4,opt,name=onlyDir"`
	Recursive  bool              `json:"recursive,omitempty" protobuf:"bytes,5,opt,name=recursive"`
	MaxDepth   int32             `json:"maxDepth,omitempty" protobuf:"bytes,6,opt,name=maxDepth"`
	FollowMove bool              `json:"followMove,omitempty" protobuf:"bytes,7,opt,name=followMove"`
	Tags       map[string]string `json:"tags,omitempty" protobuf:"bytes,8,rep,name=tags"`
}

// ArgusWatcherStatus is the status for a ArgusWatcher resource.
type ArgusWatcherStatus struct {
	ObservedGeneration int64 `json:"observedGeneration"`

	ObservablePods int32 `json:"observablePods"`
	WatchedPods    int32 `json:"watchedPods"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ArgusWatcherList is a list of ArgusWatcher resources.
type ArgusWatcherList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ArgusWatcher `json:"items"`
}
