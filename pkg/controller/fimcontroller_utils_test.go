package fimcontroller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	fimv1alpha1 "clustergarage.io/fim-controller/pkg/apis/fimcontroller/v1alpha1"
)

func TestCalculateStatus(t *testing.T) {
	fw := newFimWatcher("foo", fwMatchedLabel)
	fwStatusTests := []struct {
		name                     string
		fw                       *fimv1alpha1.FimWatcher
		filteredPods             []*corev1.Pod
		expectedFimWatcherStatus fimv1alpha1.FimWatcherStatus
	}{{
		"no matching pods",
		fw,
		[]*corev1.Pod{},
		fimv1alpha1.FimWatcherStatus{
			ObservablePods: 0,
			WatchedPods:    0,
		},
	}, {
		"1 matching pod",
		fw,
		[]*corev1.Pod{
			newPod("bar", fw, corev1.PodRunning, true, false),
		},
		fimv1alpha1.FimWatcherStatus{
			ObservablePods: 1,
			WatchedPods:    0,
		},
	}, {
		"1 matching, annotated pod",
		fw,
		[]*corev1.Pod{
			newPod("bar", fw, corev1.PodRunning, true, true),
		},
		fimv1alpha1.FimWatcherStatus{
			ObservablePods: 1,
			WatchedPods:    1,
		},
	}}

	for _, test := range fwStatusTests {
		fimWatcherStatus := calculateStatus(test.fw, test.filteredPods, nil)
		if !reflect.DeepEqual(fimWatcherStatus, test.expectedFimWatcherStatus) {
			t.Errorf("%s: unexpected FimWatcher status: expected %+v, got %+v", test.name, test.expectedFimWatcherStatus, fimWatcherStatus)
		}
	}
}

func TestUpdateAnnotations(t *testing.T) {
	fw := newFimWatcher("foo", fwMatchedLabel)
	pod := newPod("bar", fw, corev1.PodRunning, true, true)

	updateAnnotations(nil, map[string]string{FimWatcherAnnotationKey: fw.Name}, pod)
	if got, want := len(pod.Annotations), 1; got != want {
		t.Errorf("unexpected pod annotations, expected %v, got %v", want, got)
	}

	updateAnnotations([]string{FimWatcherAnnotationKey}, nil, pod)
	if got, want := len(pod.Annotations), 0; got != want {
		t.Errorf("unexpected pod annotations, expected %v, got %v", want, got)
	}
}

func TestGetPodContainerIDs(t *testing.T) {
	fw := newFimWatcher("foo", fwMatchedLabel)
	pod := newPod("bar", fw, corev1.PodRunning, true, true)

	cids := getPodContainerIDs(pod)
	if got, want := len(cids), 0; got != want {
		t.Errorf("unexpected container ids, expected %v, got %v", want, got)
	}

	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{ContainerID: "abc123"}}
	cids = getPodContainerIDs(pod)
	if got, want := len(cids), 1; got != want {
		t.Errorf("unexpected container ids, expected %v, got %v", want, got)
	}
	if cids[0] != "abc123" {
		t.Errorf("expected container id to match %v", cids[0])
	}
}
