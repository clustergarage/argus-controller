package fimcontroller

import (
	"errors"
	"fmt"
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

type FimdConnection struct {
	client pb.FimdClient
	conn   *grpc.ClientConn
	ctx    context.Context
	cancel context.CancelFunc
}

type updatePodFunc func(pod *corev1.Pod) error

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
	newStatus.ObservablePods = int32(observablePodsCount) //int32(observablePodsCount*len(fw.Spec.Subjects))
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
		glog.Infof("Pod %s/%s precondition doesn't hold, skip updating it.", namespace, name)
		retryErr = nil
	}

	return pod, retryErr
}

// @TODO: update with error return
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

func connectToFimdClient(hostURL string) FimdConnection {
	var fc FimdConnection
	var err error

	fc.conn, err = grpc.Dial(hostURL, grpc.WithInsecure())
	if err != nil {
		return fc
	}
	// @TODO: re-evaluate this timeout
	fc.ctx, fc.cancel = context.WithTimeout(context.Background(), 2*time.Second)
	fc.client = pb.NewFimdClient(fc.conn)
	return fc
}

func addFimdWatcher(hostURL string, config *pb.FimdConfig) error {
	glog.Infof("Sending CreateWatch call to FimD daemon, host: %s, request: %#v)", hostURL, config)

	fc := connectToFimdClient(hostURL)
	defer fc.conn.Close()
	defer fc.cancel()

	if fc.client == nil {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			hostURL, errors.New("could not connect to fimd client"))
	}

	response, err := fc.client.CreateWatch(fc.ctx, config)
	glog.Infof("Received CreateWatch response: %#v", response)
	if err != nil {
		return err
	}
	return nil
}

func removeFimdWatcher(hostURL string, config *pb.FimdConfig) error {
	glog.Infof("Sending DestroyWatch call to FimD daemon, host: %s, request: %#v", hostURL, config)

	fc := connectToFimdClient(hostURL)
	defer fc.conn.Close()
	defer fc.cancel()

	if fc.client == nil {
		return errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			hostURL, errors.New("could not connect to fimd client"))
	}

	_, err := fc.client.DestroyWatch(fc.ctx, config)
	if err != nil {
		return err
	}
	return nil
}

func getWatchState(hostURL string) ([]*pb.FimdHandle, error) {
	fc := connectToFimdClient(hostURL)
	defer fc.conn.Close()
	defer fc.cancel()

	if fc.client == nil {
		return nil, errorsapi.NewConflict(schema.GroupResource{Resource: "nodes"},
			hostURL, errors.New("could not connect to fimd client"))
	}

	var watchers []*pb.FimdHandle
	stream, err := fc.client.GetWatchState(fc.ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	for {
		watch, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		fmt.Println(watch)
		watchers = append(watchers, watch)
	}
	return watchers, nil
}
