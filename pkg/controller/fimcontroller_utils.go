package fimcontroller

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/golang/glog"
	"github.com/processout/grpc-go-pool"
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

// FimdConnection maps a FimD hostURL with a grpcpool.
type FimdConnection struct {
	hostURL string
	pool    *grpcpool.Pool
}

var (
	// fcInitialConnections is the amount of pool connections to intiialize
	// when creating the grpcpool.
	fcInitialConnections = 10
	// fcMaximumConnections is the maximum amount of pool connections the
	// grpcpool allows. After all connections are exhausted, it will fail to
	// connect to this pool until some connections are freed up.
	fcMaximumConnections = 50
	// fcIdleTimeout is the amount of time a pool connection can idle before
	// it is closed.
	fcIdleTimeout = 10 * time.Second
	// fcMaxLifeDuration is the amount of time before a closed connection can
	// be recycled back into the pool.
	fcMaxLifeDuration = 5 * time.Minute

	// fimdConnections is an array of stored FimdConnection mappings.
	fimdConnections []*FimdConnection
)

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

// getFimdConnection returns a FimdConnection object given a hostURL.
func getFimdConnection(hostURL string) (*FimdConnection, int, error) {
	for i, fc := range fimdConnections {
		if fc.hostURL == hostURL {
			return fc, i, nil
		}
	}
	return nil, -1, fmt.Errorf("could not connect to fimd at hostURL %v", hostURL)
}

// initFimdConnection will initialize a new FimdConnection object given a hostURL.
func initFimdConnection(hostURL string) error {
	pool, err := grpcpool.New(func() (*grpc.ClientConn, error) {
		conn, err := grpc.Dial(hostURL, grpc.WithInsecure())
		if err != nil {
			return nil, err
		}
		return conn, err
	}, fcInitialConnections, fcMaximumConnections, fcIdleTimeout, fcMaxLifeDuration)
	if err != nil {
		return err
	}

	// store in host connection pool array
	fimdConnections = append(fimdConnections, &FimdConnection{
		hostURL: hostURL,
		pool:    pool,
	})
	return nil
}

// destroyFimdConnection will remove a new FimdConnection object from the
// fimdConnections array, given a hostURL.
func destroyFimdConnection(hostURL string) error {
	fc, index, err := getFimdConnection(hostURL)
	if err != nil {
		return err
	}
	fc.pool.Close()

	fimdConnections = append(fimdConnections[:index], fimdConnections[index+1:]...)
	return nil
}

// addFimdWatcher sends a message to the FimD daemon to create a new watcher.
func addFimdWatcher(hostURL string, config *pb.FimdConfig) error {
	glog.Infof("Sending CreateWatch call to FimD daemon, host: %s, request: %#v)", hostURL, config)

	fc, _, err := getFimdConnection(hostURL)
	if err != nil {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, errors.New("failed to get fimd connection"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer ctx.Done()

	conn, err := fc.pool.Get(ctx)
	if err != nil {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, errors.New("failed to get fimd pool connection"))
	}
	client := pb.NewFimdClient(conn.ClientConn)
	defer conn.Close()

	var response *pb.FimdHandle
	response, err = client.CreateWatch(ctx, config)
	glog.Infof("Received CreateWatch response: %#v", response)
	if err != nil || response.NodeName == "" {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, errors.New("fimd::CreateWatch failed"))
	}
	return nil
}

// removeFimdWatcher sends a message to the FimD daemon to remove an existing
// watcher.
func removeFimdWatcher(hostURL string, config *pb.FimdConfig) error {
	glog.Infof("Sending DestroyWatch call to FimD daemon, host: %s, request: %#v", hostURL, config)

	fc, _, err := getFimdConnection(hostURL)
	if err != nil {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, errors.New("failed to get fimd connection"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer ctx.Done()

	conn, err := fc.pool.Get(ctx)
	if err != nil {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, errors.New("failed to get fimd pool connection"))
	}
	client := pb.NewFimdClient(conn.ClientConn)
	defer conn.Close()

	_, err = client.DestroyWatch(ctx, config)
	if err != nil {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, errors.New("fimd::DestroyWatch failed"))
	}
	return nil
}

// getWatchState sends a message to the FimD daemon to return the current state
// of the watchers being watched via inotify.
func getWatchState(hostURL string) ([]*pb.FimdHandle, error) {
	fc, _, err := getFimdConnection(hostURL)
	if err != nil {
		return nil, errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			hostURL, errors.New("failed to get fimd connection"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer ctx.Done()

	conn, err := fc.pool.Get(ctx)
	if err != nil {
		return nil, errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			hostURL, errors.New("failed to get fimd pool connection"))
	}
	client := pb.NewFimdClient(conn.ClientConn)
	defer conn.Close()

	var watchers []*pb.FimdHandle
	stream, err := client.GetWatchState(ctx, &pb.Empty{})
	if err != nil {
		return nil, errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			hostURL, errors.New("fimd::GetWatchState failed"))
	}
	for {
		watch, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		watchers = append(watchers, watch)
	}
	return watchers, nil
}
