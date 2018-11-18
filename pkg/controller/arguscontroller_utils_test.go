package arguscontroller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	argusv1alpha1 "clustergarage.io/argus-controller/pkg/apis/arguscontroller/v1alpha1"
)

func TestCalculateStatus(t *testing.T) {
	aw := newArgusWatcher("foo", awMatchedLabel)
	awStatusTests := []struct {
		name                       string
		aw                         *argusv1alpha1.ArgusWatcher
		filteredPods               []*corev1.Pod
		expectedArgusWatcherStatus argusv1alpha1.ArgusWatcherStatus
	}{{
		"no matching pods",
		aw,
		[]*corev1.Pod{},
		argusv1alpha1.ArgusWatcherStatus{
			ObservablePods: 0,
			WatchedPods:    0,
		},
	}, {
		"1 matching pod",
		aw,
		[]*corev1.Pod{
			newPod("bar", aw, corev1.PodRunning, true, false),
		},
		argusv1alpha1.ArgusWatcherStatus{
			ObservablePods: 1,
			WatchedPods:    0,
		},
	}, {
		"1 matching, annotated pod",
		aw,
		[]*corev1.Pod{
			newPod("bar", aw, corev1.PodRunning, true, true),
		},
		argusv1alpha1.ArgusWatcherStatus{
			ObservablePods: 1,
			WatchedPods:    1,
		},
	}}

	for _, test := range awStatusTests {
		argusWatcherStatus := calculateStatus(test.aw, test.filteredPods, nil)
		if !reflect.DeepEqual(argusWatcherStatus, test.expectedArgusWatcherStatus) {
			t.Errorf("%s: unexpected ArgusWatcher status: expected %+v, got %+v", test.name, test.expectedArgusWatcherStatus, argusWatcherStatus)
		}
	}
}

func TestUpdateAnnotations(t *testing.T) {
	aw := newArgusWatcher("foo", awMatchedLabel)
	pod := newPod("bar", aw, corev1.PodRunning, true, true)

	updateAnnotations(nil, map[string]string{ArgusWatcherAnnotationKey: aw.Name}, pod)
	if got, want := len(pod.Annotations), 1; got != want {
		t.Errorf("unexpected pod annotations, expected %v, got %v", want, got)
	}

	updateAnnotations([]string{ArgusWatcherAnnotationKey}, nil, pod)
	if got, want := len(pod.Annotations), 0; got != want {
		t.Errorf("unexpected pod annotations, expected %v, got %v", want, got)
	}
}

func TestGetPodContainerIDs(t *testing.T) {
	aw := newArgusWatcher("foo", awMatchedLabel)
	pod := newPod("bar", aw, corev1.PodRunning, true, true)

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
