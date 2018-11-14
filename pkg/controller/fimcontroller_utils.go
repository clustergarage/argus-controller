package fimcontroller

import (
	cryptotls "crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	prometheusMetricName = "fim_k8s_events_total"
	prometheusMetricHelp = "Count of inotify events observed by the FimD daemon."
)

var (
	// TLS ...
	TLS bool
	// TLSSkipVerify ...
	TLSSkipVerify bool
	// TLSCACert ...
	TLSCACert string
	// TLSClientCert ...
	TLSClientCert string
	// TLSClientKey ...
	TLSClientKey string
	// TLSServerName ...
	TLSServerName string

	annotationMux = &sync.RWMutex{}
)

// FimdConnection defines a FimD gRPC server URL and a gRPC client to connect
// to in order to make add and remove watcher calls, as well as getting the
// current state of the daemon to keep the controller<-->daemon in sync.
type FimdConnection struct {
	hostURL string
	handle  *pb.FimdHandle
	client  pb.FimdClient
	counter *prometheus.CounterVec
}

// BuildAndStoreDialOptions creates a grpc.DialOption object to be used with
// grpc.Dial to connect to a daemon server. This function allows both secure
// and insecure variants.
func BuildAndStoreDialOptions(tls, tlsSkipVerify bool, caCert, clientCert, clientKey, serverName string) (grpc.DialOption, error) {
	// Store passed-in configuration to later create FimdConnections.
	TLS = tls
	TLSSkipVerify = tlsSkipVerify
	TLSCACert = caCert
	TLSClientCert = clientCert
	TLSClientKey = clientKey
	TLSServerName = serverName

	if TLS {
		var tlsconfig cryptotls.Config
		if TLSClientCert != "" && TLSClientKey != "" {
			keypair, err := cryptotls.LoadX509KeyPair(TLSClientCert, TLSClientKey)
			if err != nil {
				return nil, fmt.Errorf("Failed to load TLS client cert/key pair: %v", err)
			}
			tlsconfig.Certificates = []cryptotls.Certificate{keypair}
		}

		if TLSSkipVerify {
			tlsconfig.InsecureSkipVerify = true
		} else if TLSCACert != "" {
			pem, err := ioutil.ReadFile(TLSCACert)
			if err != nil {
				return nil, fmt.Errorf("Failed to load root CA certificates from file %s: %v", TLSCACert, err)
			}
			cacertpool := x509.NewCertPool()
			if !cacertpool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("No root CA certificate parsed from file %s", TLSCACert)
			}
			tlsconfig.RootCAs = cacertpool
		}
		if TLSServerName != "" {
			tlsconfig.ServerName = TLSServerName
		}
		return grpc.WithTransportCredentials(credentials.NewTLS(&tlsconfig)), nil
	}
	return grpc.WithInsecure(), nil
}

// NewFimdConnection creates a new FimdConnection type given a required hostURL
// and an optional gRPC client; if the client is not specified, this is created
// for you here.
func NewFimdConnection(hostURL string, opts grpc.DialOption, client ...pb.FimdClient) (*FimdConnection, error) {
	fc := &FimdConnection{hostURL: hostURL}
	if len(client) > 0 {
		fc.client = client[0]
	} else {
		conn, err := grpc.Dial(fc.hostURL, opts,
			// Add Prometheus gRPC interceptors so we can monitor calls between
			// the controller and daemon.
			grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
			grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor))
		if err != nil {
			return nil, fmt.Errorf("Could not connect: %v", err)
		}
		fc.client = pb.NewFimdClient(conn)

		// Add Prometheus counter type for tracking inotify events.
		fc.counter = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: prometheusMetricName,
				Help: prometheusMetricHelp,
			},
			[]string{"fimwatcher", "event", "nodename"},
		)
		prometheus.Register(fc.counter)
		go fc.ListenForMetrics()
	}
	return fc, nil
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
		return nil, errors.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, fmt.Errorf(fmt.Sprintf("fimd::CreateWatch failed: %v", err)))
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
		return errors.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, fmt.Errorf(fmt.Sprintf("fimd::DestroyWatch failed: %v", err)))
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
		return nil, errors.NewConflict(schema.GroupResource{Resource: "nodes"},
			fc.hostURL, fmt.Errorf(fmt.Sprintf("fimd::GetWatchState failed: %v", err)))
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

// ListenForMetrics serves a stream that the daemon will send messages on when
// it receives an inotify event so we can then record it in Prometheus.
func (fc *FimdConnection) ListenForMetrics() {
	ctx := context.Background()
	defer ctx.Done()

	stream, err := fc.client.RecordMetrics(ctx, &pb.Empty{})
	if err != nil {
		return
	}
	for {
		if metric, err := stream.Recv(); err == nil {
			fc.counter.WithLabelValues(metric.FimWatcher, metric.Event, metric.NodeName).Inc()
		}
	}
}

// updatePodFunc defines an function signature to be passed into
// updatePodWithRetries.
type updatePodFunc func(pod *corev1.Pod) error

// updateFimWatcherStatus updates the status of the specified FimWatcher
// object.
func updateFimWatcherStatus(c fimv1alpha1client.FimWatcherInterface, fw *fimv1alpha1.FimWatcher,
	newStatus fimv1alpha1.FimWatcherStatus) (*fimv1alpha1.FimWatcher, error) {

	// This is the steady state. It happens when the FimWatcher doesn't have
	// any expectations, since we do a periodic relist every 30s. If the
	// generations differ but the subjects are the same, a caller might have
	// resized to the same subject count.
	if fw.Status.ObservablePods == newStatus.ObservablePods &&
		fw.Status.WatchedPods == newStatus.WatchedPods &&
		fw.Generation == fw.Status.ObservedGeneration {
		return fw, nil
	}
	// Save the generation number we acted on, otherwise we might wrongfully
	// indicate that we've seen a spec update when we retry.
	// @TODO: This can clobber an update if we allow multiple agents to write
	// to the same status.
	newStatus.ObservedGeneration = fw.Generation

	var getErr, updateErr error
	var updatedFW *fimv1alpha1.FimWatcher
	for i, fw := 0, fw; ; i++ {
		glog.V(4).Infof(fmt.Sprintf("Updating status for %v: %s/%s, ", fw.Kind, fw.Namespace, fw.Name) +
			fmt.Sprintf("observed generation %d->%d, ", fw.Status.ObservedGeneration, fw.Generation) +
			fmt.Sprintf("observable pods %d->%d, ", fw.Status.ObservablePods, newStatus.ObservablePods) +
			fmt.Sprintf("watched pods %d->%d", fw.Status.WatchedPods, newStatus.WatchedPods))

		// If the CustomResourceSubresources feature gate is not enabled, we
		// must use Update instead of UpdateStatus to update the Status block
		// of the FimWatcher resource.
		// UpdateStatus will not allow changes to the Spec of the resource,
		// which is ideal for ensuring nothing other than resource status has
		// been updated.
		fw.Status = newStatus
		updatedFW, updateErr = c.UpdateStatus(fw)
		if updateErr == nil {
			return updatedFW, nil
		}
		// Stop retrying if we exceed statusUpdateRetries - the fim watcher
		// will be requeued with a rate limit.
		if i >= statusUpdateRetries {
			break
		}
		// Update the FimWatcher with the latest resource version for the next
		// poll.
		if fw, getErr = c.Get(fw.Name, metav1.GetOptions{}); getErr != nil {
			// If the GET fails we can't trust status.ObservablePods anymore.
			// This error is bound to be more interesting than the update
			// failure.
			return nil, getErr
		}
	}

	return nil, updateErr
}

// calculateStatus creates a new status from a given FimWatcher and
// filteredPods array.
func calculateStatus(fw *fimv1alpha1.FimWatcher, filteredPods []*corev1.Pod, manageFimWatchersErr error) fimv1alpha1.FimWatcherStatus {
	newStatus := fw.Status
	newStatus.ObservablePods = int32(len(filteredPods))

	// Count the number of pods that have labels matching the labels of the pod
	// template of the fim watcher, the matching pods may have more labels than
	// are in the template. Because the label of podTemplateSpec is a superset
	// of the selector of the fim watcher, so the possible matching pods must
	// be part of the filteredPods.
	watchedPods := 0
	for _, pod := range filteredPods {
		if _, found := pod.GetAnnotations()[FimWatcherAnnotationKey]; found {
			watchedPods++
		}
	}
	newStatus.WatchedPods = int32(watchedPods)
	return newStatus
}

// updateAnnotations takes an array of annotations to remove or add and an
// object to apply this update to; this will modify the object's Annotations
// map by way of an Accessor.
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
	annotationMux.Lock()
	accessor.SetAnnotations(annotations)
	annotationMux.Unlock()

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

	// Ignore the precondition violated error, this pod is already updated with
	// the desired label.
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
