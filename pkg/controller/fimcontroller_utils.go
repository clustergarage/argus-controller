package fimcontroller

import (
	"errors"
	"io"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	errorsapi "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/retry"

	fimv1alpha1 "clustergarage.io/fim-controller/pkg/apis/fimcontroller/v1alpha1"
	fimv1alpha1client "clustergarage.io/fim-controller/pkg/client/clientset/versioned/typed/fimcontroller/v1alpha1"
	pb "github.com/clustergarage/fim-proto/golang"
)

// FimdConnection defines a FimD gRPC server URL and a gRPC client to connect
// to in order to make add and remove watcher calls, as well as getting the
// current state of the daemon to keep the controller<-->daemon in sync.
type FimdConnection struct {
	hostURL string
	handle  *pb.FimdHandle
	client  pb.FimdClient
}

// NewFimdConnection creates a new FimdConnection type given a required hostURL
// and an optional gRPC client; if the client is not specified, this is created
// for you here.
func NewFimdConnection(hostURL string, client ...pb.FimdClient) *FimdConnection {
	fc := &FimdConnection{hostURL: hostURL}
	if len(client) > 0 {
		fc.client = client[0]
	} else {
		if conn, err := grpc.Dial(fc.hostURL, grpc.WithInsecure()); err == nil {
			fc.client = pb.NewFimdClient(conn)
		}
	}
	return fc
}

// AddFimdWatcher sends a message to the FimD daemon to create a new watcher.
func (fc *FimdConnection) AddFimdWatcher(config *pb.FimdConfig) (*pb.FimdHandle, error) {
	glog.Infof("Sending CreateWatch call to FimD daemon, host: %s, request: %#v)", fc.hostURL, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer ctx.Done()

	response, err := fc.client.CreateWatch(ctx, config)
	glog.Infof("Received CreateWatch response: %#v", response)
	if err != nil || response.NodeName == "" {
		return nil, errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, errors.New("fimd::CreateWatch failed"))
	}
	return response, nil
}

// RemoveFimdWatcher sends a message to the FimD daemon to remove an existing
// watcher.
func (fc *FimdConnection) RemoveFimdWatcher(config *pb.FimdConfig) error {
	glog.Infof("Sending DestroyWatch call to FimD daemon, host: %s, request: %#v", fc.hostURL, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer ctx.Done()

	_, err := fc.client.DestroyWatch(ctx, config)
	if err != nil {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, errors.New("fimd::DestroyWatch failed"))
	}
	return nil
}

// GetWatchState sends a message to the FimD daemon to return the current state
// of the watchers being watched via inotify.
func (fc *FimdConnection) GetWatchState() ([]*pb.FimdHandle, error) {
	ctx := context.Background()
	defer ctx.Done()

	var watchers []*pb.FimdHandle
	stream, err := fc.client.GetWatchState(ctx, &pb.Empty{})
	if err != nil {
		return nil, errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			fc.hostURL, errors.New("fimd::GetWatchState failed"))
	}
	for {
		watch, err := stream.Recv()
		if err == io.EOF || err != nil {
			break
		}
		watchers = append(watchers, watch)
	}
	return watchers, nil
}

// updatePodFunc defines an function signature to be passed into
// updatePodWithRetries.
type updatePodFunc func(pod *corev1.Pod) error

// updateFimWatcherStatus updates the status of the specified FimWatcher object.
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

// calculateStatus creates a new status from a given FimWatcher and filteredPods array.
func calculateStatus(fw *fimv1alpha1.FimWatcher, filteredPods []*corev1.Pod, manageFimWatchersErr error) fimv1alpha1.FimWatcherStatus {
	newStatus := fw.Status
	// @TODO: document this
	// Count the number of pods that have labels matching the labels of the pod
	// template of the fim watcher, the matching pods may have more
	// labels than are in the template. Because the label of podTemplateSpec is
	// a superset of the selector of the fim watcher, so the possible
	// matching pods must be part of the filteredPods.
	observablePodsCount := 0
	for _, pod := range filteredPods {
		if _, found := pod.GetAnnotations()[FimWatcherAnnotationKey]; found {
			observablePodsCount++
		}
	}
	// @FIXME
	newStatus.ObservablePods = int32(observablePodsCount) //int32(observablePodsCount*len(fw.Spec.Subjects))
	return newStatus
}

// updateAnnotations takes an array of annotations to remove or add and an object
// to apply this update to; this will modify the object's Annotations map by way
// of an Accessor.
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

// updatePodWithRetries updates a pod with given applyUpdate function.
func updatePodWithRetries(podClient coreclient.PodInterface, podLister corelisters.PodLister,
	namespace, name string, applyUpdate updatePodFunc) (*corev1.Pod, error) {

	var pod *corev1.Pod

	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
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
		glog.Infof("Pod %s/%s precondition doesn't hold, skip updating it.", namespace, name)
		retryErr = nil
	}

	return pod, retryErr
}

// getPodContainerIDs returns a list of container IDs given a pod.
func getPodContainerIDs(pod *corev1.Pod) []string {
	var cids []string
	if len(pod.Status.ContainerStatuses) == 0 {
		return cids
	}
	for _, ctr := range pod.Status.ContainerStatuses {
		cids = append(cids, ctr.ContainerID)
	}
	return cids
}
