package fimcontroller

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/retry"

	fimv1alpha1 "clustergarage.io/fim-controller/pkg/apis/fimcontroller/v1alpha1"
	fimv1alpha1client "clustergarage.io/fim-controller/pkg/client/clientset/versioned/typed/fimcontroller/v1alpha1"
	pb "clustergarage.io/fim-proto/fim"
)

func updateFimWatcherStatus(c fimv1alpha1client.FimWatcherInterface, fw *fimv1alpha1.FimWatcher,
	newStatus fimv1alpha1.FimWatcherStatus) (*fimv1alpha1.FimWatcher, error) {

	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	fwCopy := fw.DeepCopy()
	//fwCopy.Status.Subjects = newStatus.Subjects
	fwCopy.Status.ObservablePods = newStatus.ObservablePods
	// If the CustomResourceSubresources feature gate is not enabled,
	// we must use Update instead of UpdateStatus to update the Status block of the FimWatcher resource.
	// UpdateStatus will not allow changes to the Spec of the resource,
	// which is ideal for ensuring nothing other than resource status has been updated.
	return fwCopy, nil
}

func calculateStatus(fw *fimv1alpha1.FimWatcher, filteredPods []*corev1.Pod, manageFimWatchersErr error) fimv1alpha1.FimWatcherStatus {
	newStatus := fw.Status
	// Count the number of pods that have labels matching the labels of the pod
	// template of the replica set, the matching pods may have more
	// labels than are in the template. Because the label of podTemplateSpec is
	// a superset of the selector of the replica set, so the possible
	// matching pods must be part of the filteredPods.
	observablePodsCount := 0
	for _, pod := range filteredPods {
		if _, found := pod.GetAnnotations()[FimWatcherAnnotationKey]; found {
			observablePodsCount++
		}
	}

	newStatus.ObservablePods = fmt.Sprintf("%d (%d subjects)", int32(observablePodsCount), int32(observablePodsCount*len(fw.Spec.Subjects)))
	return newStatus
}

func updateAnnotations(removeAnnotations []string, newAnnotations map[string]string, obj runtime.Object) error {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	annotations := accessor.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	for key, value := range newAnnotations {
		annotations[key] = value
	}
	for _, annotation := range removeAnnotations {
		delete(annotations, annotation)
	}
	accessor.SetAnnotations(annotations)

	return nil
}

type updatePodFunc func(pod *corev1.Pod) error

// UpdatePodWithRetries updates a pod with given applyUpdate function.
func updatePodWithRetries(podClient coreclient.PodInterface, podLister corelisters.PodLister,
	namespace, name string, applyUpdate updatePodFunc) (*corev1.Pod, error) {

	var pod *corev1.Pod

	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var err error
		pod, err = podLister.Pods(namespace).Get(name)
		if err != nil {
			return err
		}
		pod = pod.DeepCopy()
		// Apply the update, then attempt to push it to the apiserver.
		if applyErr := applyUpdate(pod); applyErr != nil {
			return applyErr
		}
		pod, err = podClient.Update(pod)
		return err
	})

	// Ignore the precondition violated error, this pod is already updated
	// with the desired label.
	if retryErr == errorsutil.ErrPreconditionViolated {
		glog.V(4).Infof("Pod %s/%s precondition doesn't hold, skip updating it.", namespace, name)
		retryErr = nil
	}

	return pod, retryErr
}

func getPodContainerID(pod *corev1.Pod) string {
	if len(pod.Status.ContainerStatuses) == 0 {
		return ""
	}
	// @TODO: this is assuming a single container per pod
	// eventually need to look up via podutil.GetContainerStatus(...)
	return pod.Status.ContainerStatuses[0].ContainerID
}

func addFimdWatcher(hostURL string, config *pb.FimdConfig) {
	// @TODO: send gRPC signal to [add]
	fmt.Println(" ### [gRPC] ADD:", config.ContainerId, "|", hostURL)

	conn, err := grpc.Dial(hostURL, grpc.WithInsecure())
	if err != nil {
		fmt.Printf("did not connect: %v\n", err)
		return
	}
	defer conn.Close()

	c := pb.NewFimdClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r, err := c.NewWatch(ctx, config)
	if err != nil {
		fmt.Printf("could not watch: %v\n", err)
		return
	}
	fmt.Printf("Watching: %d\n", r.Id)
}

func removeFimdWatcher(pod *corev1.Pod, cid string) {
	// @TODO: send gRPC signal to [rm]
	fmt.Println(" ### [gRPC] RM:", cid)
}
