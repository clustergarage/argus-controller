package main

import (
	//"fmt"
	//"reflect"

	//"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"

	fimv1alpha1 "clustergarage.io/fim-k8s/pkg/apis/fimcontroller/v1alpha1"
	fimv1alpha1client "clustergarage.io/fim-k8s/pkg/client/clientset/versioned/typed/fimcontroller/v1alpha1"
)

func updateFimWatcherStatus(c fimv1alpha1client.FimWatcherInterface, fw *fimv1alpha1.FimWatcher, newStatus fimv1alpha1.FimWatcherStatus) (*fimv1alpha1.FimWatcher, error) {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	fwCopy := fw.DeepCopy()
	fwCopy.Status.ReadySubjects = newStatus.ReadySubjects
	fwCopy.Status.AvailableSubjects = newStatus.AvailableSubjects
	fwCopy.Generation = fw.Status.ObservedGeneration
	// If the CustomResourceSubresources feature gate is not enabled,
	// we must use Update instead of UpdateStatus to update the Status block of the FimWatcher resource.
	// UpdateStatus will not allow changes to the Spec of the resource,
	// which is ideal for ensuring nothing other than resource status has been updated.
	return fwCopy, nil
}

func calculateStatus(fw *fimv1alpha1.FimWatcher, filteredPods []*corev1.Pod, manageFimWatchersErr error) fimv1alpha1.FimWatcherStatus {
	newStatus := fw.Status
	// Count the number of pods that have labels matching the labels of the pod
	// template of the fim watcher, the matching pods may have more
	// labels than are in the template. Because the label of podTemplateSpec is
	// a superset of the selector of the fim watcher, so the possible
	// matching pods must be part of the filteredPods.
	readySubjectsCount := 0
	availableSubjectsCount := 0
	//templateLabel := labels.Set(fw.Spec.Template.Labels).AsSelectorPreValidated()
	for _, pod := range filteredPods {
		if podutil.IsPodReady(pod) {
			readySubjectsCount++
			if podutil.IsPodAvailable(pod, 60, metav1.Now()) {
				availableSubjectsCount++
			}
		}
	}

	failureCond := GetCondition(fw.Status, fimv1alpha1.FimWatcherSubjectFailure)
	if manageFimWatchersErr != nil && failureCond == nil {
		var reason string
		if diff := len(filteredPods) - len(fw.Spec.Subjects); diff < 0 {
			reason = "FailedCreate"
		} else if diff > 0 {
			reason = "FailedDelete"
		}
		cond := NewFimWatcherCondition(fimv1alpha1.FimWatcherSubjectFailure, corev1.ConditionTrue, reason, manageFimWatchersErr.Error())
		SetCondition(&newStatus, cond)
	} else if manageFimWatchersErr == nil && failureCond != nil {
		RemoveCondition(&newStatus, fimv1alpha1.FimWatcherSubjectFailure)
	}

	newStatus.Subjects = int32(len(filteredPods))
	newStatus.ReadySubjects = int32(readySubjectsCount)
	newStatus.AvailableSubjects = int32(availableSubjectsCount)
	return newStatus
}

// NewFimWatcherCondition creates a new fim watcher condition.
func NewFimWatcherCondition(condType fimv1alpha1.FimWatcherConditionType, status corev1.ConditionStatus, reason, msg string) fimv1alpha1.FimWatcherCondition {
	return fimv1alpha1.FimWatcherCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            msg,
	}
}

// GetCondition returns a fim watcher condition with the provided type if it exists.
func GetCondition(status fimv1alpha1.FimWatcherStatus, condType fimv1alpha1.FimWatcherConditionType) *fimv1alpha1.FimWatcherCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetCondition adds/replaces the given condition in the fim watcher status. If the condition that we
// are about to add already exists and has the same status and reason then we are not going to update.
func SetCondition(status *fimv1alpha1.FimWatcherStatus, condition fimv1alpha1.FimWatcherCondition) {
	currentCond := GetCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason {
		return
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, condition)
}

// RemoveCondition removes the condition with the provided type from the fim watcher status.
func RemoveCondition(status *fimv1alpha1.FimWatcherStatus, condType fimv1alpha1.FimWatcherConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

// filterOutCondition returns a new slice of fim watcher conditions without conditions with the provided type.
func filterOutCondition(conditions []fimv1alpha1.FimWatcherCondition, condType fimv1alpha1.FimWatcherConditionType) []fimv1alpha1.FimWatcherCondition {
	var newConditions []fimv1alpha1.FimWatcherCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
