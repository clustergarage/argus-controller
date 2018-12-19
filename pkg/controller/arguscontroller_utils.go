package arguscontroller

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

	argusv1alpha1 "clustergarage.io/argus-controller/pkg/apis/arguscontroller/v1alpha1"
	argusv1alpha1client "clustergarage.io/argus-controller/pkg/client/clientset/versioned/typed/arguscontroller/v1alpha1"
	pb "github.com/clustergarage/argus-proto/golang"
)

const (
	prometheusMetricName = "argus_events_total"
	prometheusMetricHelp = "Count of inotify events observed by the ArgusD daemon."
)

var (
	// TLS specifies whether to connect to the ArgusD server using TLS.
	TLS bool
	// TLSSkipVerify specifies not to verify the certificate presented by the
	// server.
	TLSSkipVerify bool
	// TLSCACert is the file containing trusted certificates for verifying the
	// server.
	TLSCACert string
	// TLSClientCert is the file containing the client certificate for
	// authenticating with the server.
	TLSClientCert string
	// TLSClientKey is the file containing the client private key for
	// authenticating with the server.
	TLSClientKey string
	// TLSServerName overrides the hostname used to verify the server
	// certficate.
	TLSServerName string

	// prometheusCounter is registered once per controller instance, and is
	// used to increment the `prometheusMetricName` metric.
	prometheusCounter *prometheus.CounterVec

	annotationMux = &sync.RWMutex{}
)

// argusdConnection defines a ArgusD gRPC server URL and a gRPC client to
// connect to in order to make add and remove watcher calls, as well as getting
// the current state of the daemon to keep the controller<-->daemon in sync.
type argusdConnection struct {
	hostURL string
	handle  *pb.ArgusdHandle
	client  pb.ArgusdClient
}

// BuildAndStoreDialOptions creates a grpc.DialOption object to be used with
// grpc.Dial to connect to a daemon server. This function allows both secure
// and insecure variants.
func BuildAndStoreDialOptions(tls, tlsSkipVerify bool, caCert, clientCert, clientKey, serverName string) (grpc.DialOption, error) {
	// Store passed-in configuration to later create argusdConnections.
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

// NewArgusdConnection creates a new argusdConnection type given a required
// hostURL and an optional gRPC client; if the client is not specified, this is
// created for you here.
func NewArgusdConnection(hostURL string, opts grpc.DialOption, client ...pb.ArgusdClient) (*argusdConnection, error) {
	fc := &argusdConnection{hostURL: hostURL}
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
		fc.client = pb.NewArgusdClient(conn)
		go fc.ListenForMetrics()
	}
	return fc, nil
}

// NewPrometheusCounter will create and register a new Prometheus `CounterVec`
// used to increment metrics collected in this controller.
func NewPrometheusCounter() {
	// Add Prometheus counter type for tracking inotify events.
	prometheusCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheusMetricName,
			Help: prometheusMetricHelp,
		},
		[]string{"arguswatcher", "event", "nodename"},
	)
	prometheus.MustRegister(prometheusCounter)
}

// AddArgusdWatcher sends a message to the ArgusD daemon to create a new
// watcher.
func (fc *argusdConnection) AddArgusdWatcher(config *pb.ArgusdConfig) (*pb.ArgusdHandle, error) {
	glog.Infof("Sending CreateWatch call to ArgusD daemon, host: %s, request: %#v)", fc.hostURL, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer ctx.Done()

	response, err := fc.client.CreateWatch(ctx, config)
	glog.Infof("Received CreateWatch response: %#v", response)
	if err != nil || response.NodeName == "" {
		return nil, errors.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, fmt.Errorf(fmt.Sprintf("argusd::CreateWatch failed: %v", err)))
	}
	return response, nil
}

// RemoveArgusdWatcher sends a message to the ArgusD daemon to remove an
// existing watcher.
func (fc *argusdConnection) RemoveArgusdWatcher(config *pb.ArgusdConfig) error {
	glog.Infof("Sending DestroyWatch call to ArgusD daemon, host: %s, request: %#v", fc.hostURL, config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer ctx.Done()

	_, err := fc.client.DestroyWatch(ctx, config)
	if err != nil {
		return errors.NewConflict(schema.GroupResource{Resource: "nodes"},
			config.NodeName, fmt.Errorf(fmt.Sprintf("argusd::DestroyWatch failed: %v", err)))
	}
	return nil
}

// GetWatchState sends a message to the ArgusD daemon to return the current
// state of the watchers being watched via inotify.
func (fc *argusdConnection) GetWatchState() ([]*pb.ArgusdHandle, error) {
	ctx := context.Background()
	defer ctx.Done()

	var watchers []*pb.ArgusdHandle
	stream, err := fc.client.GetWatchState(ctx, &pb.Empty{})
	if err != nil {
		return nil, errors.NewConflict(schema.GroupResource{Resource: "nodes"},
			fc.hostURL, fmt.Errorf(fmt.Sprintf("argusd::GetWatchState failed: %v", err)))
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
func (fc *argusdConnection) ListenForMetrics() {
	ctx := context.Background()
	defer ctx.Done()

	stream, err := fc.client.RecordMetrics(ctx, &pb.Empty{})
	if err != nil {
		return
	}
	for {
		metric, err := stream.Recv()
		// If the connection to the daemon is severed, we don't wish for this
		// to continue looping.
		if err != nil {
			return
		}
		prometheusCounter.WithLabelValues(metric.ArgusWatcher, metric.Event, metric.NodeName).Inc()
	}
}

// updatePodFunc defines an function signature to be passed into
// updatePodWithRetries.
type updatePodFunc func(pod *corev1.Pod) error

// updateArgusWatcherStatus updates the status of the specified ArgusWatcher
// object.
func updateArgusWatcherStatus(c argusv1alpha1client.ArgusWatcherInterface, aw *argusv1alpha1.ArgusWatcher,
	newStatus argusv1alpha1.ArgusWatcherStatus) (*argusv1alpha1.ArgusWatcher, error) {

	// This is the steady state. It happens when the ArgusWatcher doesn't have
	// any expectations, since we do a periodic relist every 30s. If the
	// generations differ but the subjects are the same, a caller might have
	// resized to the same subject count.
	if aw.Status.ObservablePods == newStatus.ObservablePods &&
		aw.Status.WatchedPods == newStatus.WatchedPods &&
		aw.Generation == aw.Status.ObservedGeneration {
		return aw, nil
	}
	// Save the generation number we acted on, otherwise we might wrongfully
	// indicate that we've seen a spec update when we retry.
	// @TODO: This can clobber an update if we allow multiple agents to write
	// to the same status.
	newStatus.ObservedGeneration = aw.Generation

	var getErr, updateErr error
	var updatedAW *argusv1alpha1.ArgusWatcher
	for i, aw := 0, aw; ; i++ {
		glog.V(4).Infof(fmt.Sprintf("Updating status for %v: %s/%s, ", aw.Kind, aw.Namespace, aw.Name) +
			fmt.Sprintf("observed generation %d->%d, ", aw.Status.ObservedGeneration, aw.Generation) +
			fmt.Sprintf("observable pods %d->%d, ", aw.Status.ObservablePods, newStatus.ObservablePods) +
			fmt.Sprintf("watched pods %d->%d", aw.Status.WatchedPods, newStatus.WatchedPods))

		// If the CustomResourceSubresources feature gate is not enabled, we
		// must use Update instead of UpdateStatus to update the Status block
		// of the ArgusWatcher resource.
		// UpdateStatus will not allow changes to the Spec of the resource,
		// which is ideal for ensuring nothing other than resource status has
		// been updated.
		aw.Status = newStatus
		updatedAW, updateErr = c.UpdateStatus(aw)
		if updateErr == nil {
			return updatedAW, nil
		}
		// Stop retrying if we exceed statusUpdateRetries - the argus watcher
		// will be requeued with a rate limit.
		if i >= statusUpdateRetries {
			break
		}
		// Update the ArgusWatcher with the latest resource version for the next
		// poll.
		if aw, getErr = c.Get(aw.Name, metav1.GetOptions{}); getErr != nil {
			// If the GET fails we can't trust status.ObservablePods anymore.
			// This error is bound to be more interesting than the update
			// failure.
			return nil, getErr
		}
	}

	return nil, updateErr
}

// calculateStatus creates a new status from a given ArgusWatcher and
// filteredPods array.
func calculateStatus(aw *argusv1alpha1.ArgusWatcher, filteredPods []*corev1.Pod, manageArgusWatchersErr error) argusv1alpha1.ArgusWatcherStatus {
	newStatus := aw.Status
	newStatus.ObservablePods = int32(len(filteredPods))

	// Count the number of pods that have labels matching the labels of the pod
	// template of the argus watcher, the matching pods may have more labels
	// than are in the template. Because the label of podTemplateSpec is a
	// superset of the selector of the argus watcher, so the possible matching
	// pods must be part of the filteredPods.
	watchedPods := 0
	for _, pod := range filteredPods {
		if _, found := pod.GetAnnotations()[ArgusWatcherAnnotationKey]; found {
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
