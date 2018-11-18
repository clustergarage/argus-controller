package arguscontroller

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	errorsutil "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"k8s.io/kubernetes/pkg/controller"

	argusv1alpha1 "clustergarage.io/argus-controller/pkg/apis/arguscontroller/v1alpha1"
	clientset "clustergarage.io/argus-controller/pkg/client/clientset/versioned"
	argusscheme "clustergarage.io/argus-controller/pkg/client/clientset/versioned/scheme"
	informers "clustergarage.io/argus-controller/pkg/client/informers/externalversions/arguscontroller/v1alpha1"
	listers "clustergarage.io/argus-controller/pkg/client/listers/arguscontroller/v1alpha1"
	pb "github.com/clustergarage/argus-proto/golang"
)

const (
	arguscontrollerAgentName = "argus-controller"
	argusNamespace           = "argus"
	argusdService            = "argusd-svc"
	argusdSvcPortName        = "grpc"

	// ArgusWatcherAnnotationKey value to annotate a pod being watched by a
	// ArgusD daemon.
	ArgusWatcherAnnotationKey = "clustergarage.io/argus-watcher"

	// SuccessSynced is used as part of the Event 'reason' when a ArgusWatcher
	// is synced.
	SuccessSynced = "Synced"
	// SuccessAdded is used as part of the Event 'reason' when a ArgusWatcher
	// is synced.
	SuccessAdded = "Added"
	// SuccessRemoved is used as part of the Event 'reason' when a ArgusWatcher
	// is synced.
	SuccessRemoved = "Removed"
	// MessageResourceAdded is the message used for an Event fired when a
	// ArgusWatcher is synced added.
	MessageResourceAdded = "Added ArgusD watcher on %v"
	// MessageResourceRemoved is the message used for an Event fired when a
	// ArgusWatcher is synced removed.
	MessageResourceRemoved = "Removed ArgusD watcher on %v"
	// MessageResourceSynced is the message used for an Event fired when a
	// ArgusWatcher is synced successfully.
	MessageResourceSynced = "ArgusWatcher synced successfully"

	// statusUpdateRetries is the number of times we retry updating a
	// ArgusWatcher's status.
	statusUpdateRetries = 1
	// minReadySeconds
	minReadySeconds = 10
)

var (
	argusdSelector = map[string]string{"daemon": "argusd"}

	// updatePodQueue stores a local queue of pod updates that will ensure pods
	// aren't being updated more than once at a single time. For example: if we
	// get an addPod event for a daemon, which checks any pods that need an
	// update, and another addPod event comes through for the pod that's
	// already being updated, it won't queue again until it finishes processing
	// and removes itself from the queue.
	updatePodQueue sync.Map
)

// ArgusWatcherController is the controller implementation for ArgusWatcher
// resources.
type ArgusWatcherController struct {
	// GroupVersionKind indicates the controller type.
	// Different instances of this struct may handle different GVKs.
	schema.GroupVersionKind

	// kubeclientset is a standard Kubernetes clientset.
	kubeclientset kubernetes.Interface
	// argusclientset is a clientset for our own API group.
	argusclientset clientset.Interface

	// Allow injection of syncArgusWatcher.
	syncHandler func(key string) error
	// backoff is the backoff definition for RetryOnConflict.
	backoff wait.Backoff

	// A TTLCache of pod creates/deletes each aw expects to see.
	expectations *controller.UIDTrackingControllerExpectations

	// A store of ArgusWatchers, populated by the shared informer passed to
	// NewArgusWatcherController.
	awLister listers.ArgusWatcherLister
	// awListerSynced returns true if the pod store has been synced at least
	// once. Added as a member to the struct to allow injection for testing.
	awListerSynced cache.InformerSynced

	// A store of pods, populated by the shared informer passed to
	// NewArgusWatcherController.
	podLister corelisters.PodLister
	// podListerSynced returns true if the pod store has been synced at least
	// once. Added as a member to the struct to allow injection for testing.
	podListerSynced cache.InformerSynced

	// A store of endpoints, populated by the shared informer passed to
	// NewArgusWatcherController.
	endpointsLister corelisters.EndpointsLister
	// endpointListerSynced returns true if the endpoints store has been synced
	// at least once. Added as a member to the struct to allow injection for
	// testing.
	endpointsListerSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	// argusdConnections is a collection of connections we have open to the
	// ArgusD server which wrap important functions to add, remove, and get a
	// current up-to-date state of what the daemon thinks it should be
	// watching.
	argusdConnections sync.Map
	// argusdURL is used to connect to the ArgusD gRPC server if daemon is
	// out-of-cluster.
	argusdURL string
}

// NewArgusWatcherController returns a new ArgusWatcher controller.
func NewArgusWatcherController(kubeclientset kubernetes.Interface, argusclientset clientset.Interface,
	awInformer informers.ArgusWatcherInformer, podInformer coreinformers.PodInformer,
	endpointsInformer coreinformers.EndpointsInformer, argusdConnection *argusdConnection) *ArgusWatcherController {

	// Create event broadcaster.
	// Add arguscontroller types to the default Kubernetes Scheme so Events can
	// be logged for arguscontroller types.
	argusscheme.AddToScheme(scheme.Scheme)
	glog.Info("Creating event broadcaster")

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: arguscontrollerAgentName})

	awc := &ArgusWatcherController{
		GroupVersionKind:      appsv1.SchemeGroupVersion.WithKind("ArgusWatcher"),
		kubeclientset:         kubeclientset,
		argusclientset:        argusclientset,
		expectations:          controller.NewUIDTrackingControllerExpectations(controller.NewControllerExpectations()),
		awLister:              awInformer.Lister(),
		awListerSynced:        awInformer.Informer().HasSynced,
		podLister:             podInformer.Lister(),
		podListerSynced:       podInformer.Informer().HasSynced,
		endpointsLister:       endpointsInformer.Lister(),
		endpointsListerSynced: endpointsInformer.Informer().HasSynced,
		workqueue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ArgusWatchers"),
		recorder:              recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when ArgusWatcher resources change.
	awInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    awc.enqueueArgusWatcher,
		UpdateFunc: awc.updateArgusWatcher,
		DeleteFunc: awc.enqueueArgusWatcher,
	})

	// Set up an event handler for when Pod resources change.
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: awc.addPod,
		// This invokes the ArgusWatcher for every pod change, eg: host
		// assignment. Though this might seem like overkill the most frequent
		// pod update is status, and the associated ArgusWatcher will only list
		// from local storage, so it should be okay.
		UpdateFunc: awc.updatePod,
		DeleteFunc: awc.deletePod,
	})

	awc.syncHandler = awc.syncArgusWatcher
	awc.backoff = wait.Backoff{
		Steps:    10,
		Duration: 1 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
	}

	// If specifying a argusdConnection for a daemon that is located
	// out-of-cluster, initialize the argusd connection here, because we will
	// not receive an addPod event where it is normally initialized.
	awc.argusdConnections = sync.Map{}
	if argusdConnection != nil {
		awc.argusdConnections.Store(argusdConnection.hostURL, argusdConnection)
		awc.argusdURL = argusdConnection.hostURL
	}

	return awc
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (awc *ArgusWatcherController) Run(workers int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer awc.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches.
	glog.Info("Starting ArgusWatcher controller")
	defer glog.Info("Shutting down ArgusWatcher controller")

	// Wait for the caches to be synced before starting workers.
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, awc.podListerSynced, awc.endpointsListerSynced, awc.awListerSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process ArgusWatcher resources.
	for i := 0; i < workers; i++ {
		go wait.Until(awc.runWorker, time.Second, stopCh)
	}
	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// updateArgusWatcher is a callback for when a ArgusWatcher is updated.
func (awc *ArgusWatcherController) updateArgusWatcher(old, new interface{}) {
	oldAW := old.(*argusv1alpha1.ArgusWatcher)
	newAW := new.(*argusv1alpha1.ArgusWatcher)

	logFormatChanged := !reflect.DeepEqual(newAW.Spec.LogFormat, oldAW.Spec.LogFormat)
	subjectsChanged := !reflect.DeepEqual(newAW.Spec.Subjects, oldAW.Spec.Subjects)

	if logFormatChanged || subjectsChanged {
		// Add new ArgusWatcher definitions.
		selector, err := metav1.LabelSelectorAsSelector(newAW.Spec.Selector)
		if err != nil {
			return
		}
		if selectedPods, err := awc.podLister.Pods(newAW.Namespace).List(selector); err == nil {
			for _, pod := range selectedPods {
				if !podutil.IsPodReady(pod) {
					continue
				}
				go awc.updatePodOnceValid(pod.Name, newAW)
			}
		}
	}

	awc.enqueueArgusWatcher(newAW)
}

// addPod is called when a pod is created, enqueue the ArgusWatcher that
// manages it and update its expectations.
func (awc *ArgusWatcherController) addPod(obj interface{}) {
	pod := obj.(*corev1.Pod)

	if pod.DeletionTimestamp != nil {
		// On a restart of the controller manager, it's possible a new pod
		// shows up in a state that is already pending deletion. Prevent the
		// pod from being a creation observation.
		awc.deletePod(pod)
		return
	}

	// If it has a ArgusWatcher annotation that's all that matters.
	if awName, found := pod.Annotations[ArgusWatcherAnnotationKey]; found {
		aw, err := awc.awLister.ArgusWatchers(pod.Namespace).Get(awName)
		if err != nil {
			return
		}
		awKey, err := controller.KeyFunc(aw)
		if err != nil {
			return
		}
		glog.V(4).Infof("Pod %s created: %#v.", pod.Name, pod)
		awc.expectations.CreationObserved(awKey)
		awc.enqueueArgusWatcher(aw)
		return
	}

	// If this pod is a ArgusD pod, we need to first initialize the connection
	// to the gRPC server run on the daemon. Then a check is done on any pods
	// running on the same node as the daemon, if they match our nodeSelector
	// then immediately enqueue the ArgusWatcher for additions.
	if label, _ := pod.Labels["daemon"]; label == "argusd" {
		var hostURL string
		// Run this function with a retry, to make sure we get a connection to
		// the daemon pod. If we exhaust all attempts, process error
		// accordingly.
		if retryErr := retry.RetryOnConflict(awc.backoff, func() (err error) {
			po, err := awc.podLister.Pods(argusNamespace).Get(pod.Name)
			if err != nil {
				err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
					po.Name, errors.New("could not find pod"))
				return err
			}
			hostURL, err = awc.getHostURL(po)
			if err != nil {
				err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
					po.Name, errors.New("pod host is not available"))
				return err
			}
			// Initialize connection to gRPC server on daemon.
			opts, err := BuildAndStoreDialOptions(TLS, TLSSkipVerify, TLSCACert, TLSClientCert, TLSClientKey, TLSServerName)
			if err != nil {
				return err
			}
			conn, err := NewArgusdConnection(hostURL, opts)
			if err != nil {
				return err
			}
			awc.argusdConnections.Store(hostURL, conn)
			return err
		}); retryErr != nil {
			return
		}

		allPods, err := awc.podLister.List(labels.Everything())
		if err != nil {
			return
		}
		for _, po := range allPods {
			if po.Spec.NodeName != pod.Spec.NodeName {
				continue
			}
			aws := awc.getPodArgusWatchers(po)
			if len(aws) == 0 {
				continue
			}

			glog.V(4).Infof("Unannotated pod %s found: %#v.", po.Name, po)
			for _, aw := range aws {
				awc.enqueueArgusWatcher(aw)
			}
		}
		return
	}

	// Get a list of all matching ArgusWatchers and sync them. Do not observe
	// creation because no controller should be waiting for an orphan.
	aws := awc.getPodArgusWatchers(pod)
	if len(aws) == 0 {
		return
	}

	glog.V(4).Infof("Unannotated pod %s found: %#v.", pod.Name, pod)
	for _, aw := range aws {
		awc.enqueueArgusWatcher(aw)
	}
}

// updatePod is called when a pod is updated. Figure out what ArgusWatcher(s)
// manage it and wake them up. If the labels of the pod have changed we need to
// awaken both the old and new ArgusWatcher. old and new must be *corev1.Pod
// types.
func (awc *ArgusWatcherController) updatePod(old, new interface{}) {
	newPod := new.(*corev1.Pod)
	oldPod := old.(*corev1.Pod)

	if newPod.ResourceVersion == oldPod.ResourceVersion {
		// Periodic resync will send update events for all known pods.
		// Two different versions of the same pod will always have different
		// ResourceVersions.
		return
	}

	labelChanged := !reflect.DeepEqual(newPod.Labels, oldPod.Labels)
	if newPod.DeletionTimestamp != nil {
		// When a pod is deleted gracefully it's deletion timestamp is first
		// modified to reflect a grace period, and after such time has passed,
		// the kubelet actually deletes it from the store. We receive an update
		// for modification of the deletion timestamp and expect an aw to
		// create more watchers asap, not wait until the kubelet actually
		// deletes the pod. This is different from the Phase of a pod changing,
		// because an aw never initiates a phase change, and so is never asleep
		// waiting for the same.
		awc.deletePod(newPod)
		if labelChanged {
			// We don't need to check the oldPod.DeletionTimestamp because
			// DeletionTimestamp cannot be unset.
			awc.deletePod(oldPod)
		}
		return
	}

	aws := awc.getPodArgusWatchers(newPod)
	for _, aw := range aws {
		awc.enqueueArgusWatcher(aw)
	}
}

// deletePod is called when a pod is deleted. Enqueue the ArgusWatcher that
// watches the pod and update its expectations. obj could be an *v1.Pod, or a
// DeletionFinalStateUnknown marker item.
func (awc *ArgusWatcherController) deletePod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)

	// When a delete is dropped, the relist will notice a pod in the store not
	// in the list, leading to the insertion of a tombstone object which
	// contains the deleted key/value. Note that this value might be stale. If
	// the pod changed labels the new ArgusWatcher will not be woken up until
	// the periodic resync.
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("couldn't get object from tombstone %+v", obj))
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			runtime.HandleError(fmt.Errorf("tombstone contained object that is not a pod %#v", obj))
			return
		}
	}

	if awName, found := pod.Annotations[ArgusWatcherAnnotationKey]; found {
		aw, err := awc.awLister.ArgusWatchers(pod.Namespace).Get(awName)
		if err != nil {
			return
		}
		awKey, err := controller.KeyFunc(aw)
		if err != nil {
			return
		}
		glog.V(4).Infof("Annotated pod %s/%s deleted through %v, timestamp %+v: %#v.",
			pod.Namespace, pod.Name, runtime.GetCaller(), pod.DeletionTimestamp, pod)
		awc.expectations.DeletionObserved(awKey, controller.PodKey(pod))
		awc.enqueueArgusWatcher(aw)
	}

	// If this pod is a ArgusD pod, we need to first destroy the connection to
	// the gRPC server run on the daemon. Then remove relevant ArgusWatcher
	// annotations from pods on the same node.
	if label, _ := pod.Labels["daemon"]; label == "argusd" {
		hostURL, err := awc.getHostURL(pod)
		if err != nil {
			return
		}
		// Destroy connections to gRPC server on daemon.
		awc.argusdConnections.Delete(hostURL)

		allPods, err := awc.podLister.List(labels.Everything())
		if err != nil {
			return
		}
		for _, po := range allPods {
			if po.Spec.NodeName != pod.Spec.NodeName {
				continue
			}
			//delete(po.Annotations, ArgusWatcherAnnotationKey)
			updateAnnotations([]string{ArgusWatcherAnnotationKey}, nil, po)
		}
		glog.V(4).Infof("Daemon pod %s/%s deleted through %v, timestamp %+v: %#v.",
			pod.Namespace, pod.Name, runtime.GetCaller(), pod.DeletionTimestamp, pod)
	}
}

// enqueueArgusWatcher takes a ArgusWatcher resource and converts it into a
// namespace/name string which is then put onto the workqueue. This method
// should not be passed resources of any type other than ArgusWatcher.
func (awc *ArgusWatcherController) enqueueArgusWatcher(obj interface{}) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		runtime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", obj, err))
		return
	}
	awc.workqueue.AddRateLimited(key)
}

// enqueueArgusWatcherAfter ...
func (awc *ArgusWatcherController) enqueueArgusWatcherAfter(obj interface{}, after time.Duration) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		runtime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", obj, err))
		return
	}
	awc.workqueue.AddAfter(key, after)
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (awc *ArgusWatcherController) runWorker() {
	for awc.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (awc *ArgusWatcherController) processNextWorkItem() bool {
	obj, shutdown := awc.workqueue.Get()
	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer awc.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished processing
		// this item. We also must remember to call Forget if we do not want
		// this work item being re-queued. For example, we do not call Forget
		// if a transient error occurs, instead the item is put back on the
		// workqueue and attempted again after a back-off period.
		defer awc.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the form
		// namespace/name. We do this as the delayed nature of the workqueue
		// means the items in the informer cache may actually be more up to
		// date that when the item was initially put onto the workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call Forget
			// here else we'd go into a loop of attempting to process a work
			// item that is invalid.
			awc.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("Expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// ArgusWatcher resource to be synced.
		if err := awc.syncHandler(key); err != nil {
			return fmt.Errorf("Error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not get
		// queued again until another change happens.
		awc.workqueue.Forget(obj)
		glog.V(4).Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		awc.workqueue.AddRateLimited(obj)
		return true
	}
	return true
}

// syncArgusWatcher compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the ArgusWatcher
// resource with the current status of the resource.
func (awc *ArgusWatcherController) syncArgusWatcher(key string) error {
	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing %v %q (%v)", awc.Kind, key, time.Since(startTime))
	}()

	// Convert the namespace/name string into a distinct namespace and name.
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the ArgusWatcher resource with this namespace/name.
	aw, err := awc.awLister.ArgusWatchers(namespace).Get(name)
	// The ArgusWatcher resource may no longer exist, in which case we stop
	// processing.
	if errorsutil.IsNotFound(err) {
		// @TODO: cleanup: delete annotations from any pods that have them
		runtime.HandleError(fmt.Errorf("%v '%s' in work queue no longer exists", awc.Kind, key))
		awc.expectations.DeleteExpectations(key)
		return nil
	}
	if err != nil {
		return err
	}

	awNeedsSync := awc.expectations.SatisfiedExpectations(key)

	// Get the diff between all pods and pods that match the ArgusWatch
	// selector.
	var rmPods []*corev1.Pod
	var addPods []*corev1.Pod

	selector, err := metav1.LabelSelectorAsSelector(aw.Spec.Selector)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Error converting pod selector to selector: %v", err))
		return nil
	}
	selectedPods, err := awc.podLister.Pods(aw.Namespace).List(selector)
	if err != nil {
		return err
	}

	// @TODO: Only get pods with annotation: ArgusWatcherAnnotationKey.
	allPods, err := awc.podLister.Pods(aw.Namespace).List(labels.Everything())
	if err != nil {
		return err
	}

	// Get current watch state from ArgusD daemon.
	watchStates, err := awc.getWatchStates()
	if err != nil {
		return err
	}

	for _, pod := range selectedPods {
		if pod.DeletionTimestamp != nil ||
			!podutil.IsPodReady(pod) {
			continue
		}
		if wsFound := awc.isPodInWatchState(pod, watchStates); !wsFound {
			var found bool
			updatePodQueue.Range(func(k, v interface{}) bool {
				// Check if pod is already in updatePodQueue.
				if pod.Name == k {
					found = true
					return false
				}
				return true
			})
			if !found {
				addPods = append(addPods, pod)
				continue
			}
		}
	}

	for _, pod := range allPods {
		if wsFound := awc.isPodInWatchState(pod, watchStates); !wsFound {
			continue
		}

		var selFound bool
		for _, po := range selectedPods {
			if pod.Name == po.Name {
				selFound = true
				break
			}
		}
		if pod.DeletionTimestamp != nil || !selFound {
			if value, found := pod.Annotations[ArgusWatcherAnnotationKey]; found && value == aw.Name {
				rmPods = append(rmPods, pod)
				continue
			}
		}
	}

	var manageSubjectsErr error
	if (awNeedsSync && aw.DeletionTimestamp == nil) ||
		len(rmPods) > 0 ||
		len(addPods) > 0 {
		manageSubjectsErr = awc.manageObserverPods(rmPods, addPods, aw)
	}

	aw = aw.DeepCopy()
	newStatus := calculateStatus(aw, selectedPods, manageSubjectsErr)

	// Always updates status as pods come up or die.
	updatedAW, err := updateArgusWatcherStatus(awc.argusclientset.ArguscontrollerV1alpha1().ArgusWatchers(aw.Namespace), aw, newStatus)
	if err != nil {
		// Multiple things could lead to this update failing. Requeuing the
		// argus watcher ensures. Returning an error causes a requeue without
		// forcing a hotloop.
		return err
	}

	// Resync the ArgusWatcher after MinReadySeconds as a last line of defense
	// to guard against clock-skew.
	if manageSubjectsErr == nil &&
		minReadySeconds > 0 &&
		updatedAW.Status.WatchedPods != int32(len(selectedPods)) {
		awc.enqueueArgusWatcherAfter(updatedAW, time.Duration(minReadySeconds)*time.Second)
	}
	return manageSubjectsErr
}

// manageObserverPods checks and updates observers for the given ArgusWatcher.
// It will requeue the ArgusWatcher in case of an error while creating/deleting
// pods.
func (awc *ArgusWatcherController) manageObserverPods(rmPods []*corev1.Pod, addPods []*corev1.Pod, aw *argusv1alpha1.ArgusWatcher) error {
	awKey, err := controller.KeyFunc(aw)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Couldn't get key for %v %#v: %v", awc.Kind, aw, err))
		return nil
	}

	if len(rmPods) > 0 {
		awc.expectations.ExpectDeletions(awKey, getPodKeys(rmPods))
		glog.Infof("Too many watchers for %v %s/%s, deleting %d", awc.Kind, aw.Namespace, aw.Name, len(rmPods))
	}
	if len(addPods) > 0 {
		awc.expectations.ExpectCreations(awKey, len(addPods))
		glog.Infof("Too few watchers for %v %s/%s, creating %d", awc.Kind, aw.Namespace, aw.Name, len(addPods))
	}

	var podsToUpdate []*corev1.Pod

	for _, pod := range rmPods {
		if _, found := pod.Annotations[ArgusWatcherAnnotationKey]; found {
			cids := getPodContainerIDs(pod)
			if len(cids) > 0 {
				hostURL, err := awc.getHostURLFromSiblingPod(pod)
				if err != nil {
					return err
				}
				fc, err := awc.getArgusdConnection(hostURL)
				if err != nil {
					return err
				}
				if fc.handle == nil {
					return fmt.Errorf("argusd connection has no handle %#v", fc)
				}

				if err := fc.RemoveArgusdWatcher(&pb.ArgusdConfig{
					NodeName: pod.Spec.NodeName,
					PodName:  pod.Name,
					Pid:      fc.handle.Pid,
				}); err != nil {
					return err
				}

				awc.expectations.DeletionObserved(awKey, controller.PodKey(pod))
				awc.recorder.Eventf(aw, corev1.EventTypeNormal, SuccessRemoved, MessageResourceRemoved, pod.Spec.NodeName)
			}
		}

		err := updateAnnotations([]string{ArgusWatcherAnnotationKey}, nil, pod)
		if err != nil {
			return err
		}
		podsToUpdate = append(podsToUpdate, pod)
	}

	for _, pod := range addPods {
		go awc.updatePodOnceValid(pod.Name, aw)

		err := updateAnnotations(nil, map[string]string{ArgusWatcherAnnotationKey: aw.Name}, pod)
		if err != nil {
			return err
		}
		podsToUpdate = append(podsToUpdate, pod)
		// Once updatePodOnceValid is called, put it in an update queue so we
		// don't start another RetryOnConflict while one is already in effect.
		updatePodQueue.Store(pod.Name, true)
	}

	for _, pod := range podsToUpdate {
		updatePodWithRetries(awc.kubeclientset.CoreV1().Pods(pod.Namespace), awc.podLister,
			aw.Namespace, pod.Name, func(po *corev1.Pod) error {
				po.Annotations = pod.Annotations
				return nil
			})
	}

	return nil
}

// getPodArgusWatchers returns a list of ArgusWatchers matching the given pod.
func (awc *ArgusWatcherController) getPodArgusWatchers(pod *corev1.Pod) []*argusv1alpha1.ArgusWatcher {
	if len(pod.Labels) == 0 {
		glog.V(4).Infof("no ArgusWatchers found for pod %v because it has no labels", pod.Name)
		return nil
	}

	list, err := awc.awLister.ArgusWatchers(pod.Namespace).List(labels.Everything())
	if err != nil {
		return nil
	}

	var aws []*argusv1alpha1.ArgusWatcher
	for _, aw := range list {
		if aw.Namespace != pod.Namespace {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(aw.Spec.Selector)
		if err != nil {
			runtime.HandleError(fmt.Errorf("invalid selector: %v", err))
			return nil
		}
		// If a ArgusWatcher with a nil or empty selector creeps in, it should
		// match nothing, not everything.
		if selector.Empty() ||
			!selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		aws = append(aws, aw)
	}

	if len(aws) == 0 {
		glog.V(4).Infof("could not find ArgusWatcher for pod %s in namespace %s with labels: %v", pod.Name, pod.Namespace, pod.Labels)
		return nil
	}
	if len(aws) > 1 {
		// ControllerRef will ensure we don't do anything crazy, but more than
		//one item in this list nevertheless constitutes user error.
		runtime.HandleError(fmt.Errorf("user error; more than one %v is selecting pods with labels: %+v", awc.Kind, pod.Labels))
	}
	return aws
}

// getPodKeys returns a list of pod key strings from array of pod objects.
func getPodKeys(pods []*corev1.Pod) []string {
	podKeys := make([]string, 0, len(pods))
	for _, pod := range pods {
		podKeys = append(podKeys, controller.PodKey(pod))
	}
	return podKeys
}

// updatePodOnceValid first retries getting the pod hostURL to connect to the
// ArgusD gRPC server. Then it retries adding a new ArgusD watcher by calling
// the gRPC server CreateWatch function; on success it updates the appropriate
// ArgusWatcher annotations so we can mark it now "watched".
func (awc *ArgusWatcherController) updatePodOnceValid(podName string, aw *argusv1alpha1.ArgusWatcher) {
	var cids []string
	var nodeName, hostURL string

	// Run this function with a retry, to make sure we get a connection to the
	// daemon pod. If we exhaust all attempts, process error accordingly.
	if retryErr := retry.RetryOnConflict(awc.backoff, func() (err error) {
		// Be sure to clear all slice elements first in case of a retry.
		cids = cids[:0]

		pod, err := awc.podLister.Pods(aw.Namespace).Get(podName)
		if err != nil {
			return err
		}
		if pod.Spec.NodeName == "" || pod.Status.HostIP == "" {
			err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
				pod.Name, errors.New("host name/ip not available"))
			return err
		}
		nodeName = pod.Spec.NodeName

		for _, ctr := range pod.Status.ContainerStatuses {
			if ctr.ContainerID == "" {
				err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
					pod.Name, errors.New("pod container id not available"))
				return err
			}
			cids = append(cids, ctr.ContainerID)
		}
		if len(pod.Spec.Containers) != len(cids) {
			err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
				pod.Name, errors.New("available pod container count does not match ready"))
			return err
		}

		var hostErr error
		hostURL, hostErr = awc.getHostURLFromSiblingPod(pod)
		if hostErr != nil || hostURL == "" {
			err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
				pod.Name, errors.New("pod host is not available"))
		}
		return err
	}); retryErr != nil {
		return
	}

	// Run this function with a retry, to make sure we get a successful
	// response from the daemon. If we exhaust all attempts, process error
	// accordingly.
	if retryErr := retry.RetryOnConflict(awc.backoff, func() (err error) {
		pod, err := awc.podLister.Pods(aw.Namespace).Get(podName)
		if err != nil {
			return err
		}
		if pod.DeletionTimestamp != nil {
			return fmt.Errorf("pod is being deleted %v", pod.Name)
		}

		fc, err := awc.getArgusdConnection(hostURL)
		if err != nil {
			return errorsutil.NewConflict(schema.GroupResource{Resource: "nodes"},
				nodeName, errors.New("failed to get argusd connection"))
		}
		if handle, err := fc.AddArgusdWatcher(&pb.ArgusdConfig{
			Name:      aw.Name,
			NodeName:  nodeName,
			PodName:   pod.Name,
			Cid:       cids,
			Subject:   awc.getArgusWatcherSubjects(aw),
			LogFormat: aw.Spec.LogFormat,
		}); err == nil {
			fc.handle = handle
		}
		return err
	}); retryErr != nil {
		updatePodWithRetries(awc.kubeclientset.CoreV1().Pods(aw.Namespace), awc.podLister,
			aw.Namespace, podName, func(po *corev1.Pod) error {
				pod, err := awc.podLister.Pods(aw.Namespace).Get(podName)
				if err != nil {
					return err
				}
				po.Annotations = pod.Annotations
				return nil
			})
	}

	go awc.removePodFromUpdateQueue(podName)

	awKey, err := controller.KeyFunc(aw)
	if err != nil {
		return
	}
	awc.expectations.CreationObserved(awKey)
	awc.recorder.Eventf(aw, corev1.EventTypeNormal, SuccessAdded, MessageResourceAdded, nodeName)
}

// getHostURL constructs a URL from the pod's hostIP and hard-coded ArgusD
// port.  The pod specified in this function is assumed to be a daemon pod.
// If argusdURL was specified to the controller, to connect to an
// out-of-cluster daemon, use this instead.
func (awc *ArgusWatcherController) getHostURL(pod *corev1.Pod) (string, error) {
	if awc.argusdURL != "" {
		return awc.argusdURL, nil
	}

	if pod.Status.PodIP == "" {
		return "", fmt.Errorf("cannot locate argusd pod on node %v", pod.Spec.NodeName)
	}

	endpoint, err := awc.endpointsLister.Endpoints(argusNamespace).Get(argusdService)
	if err != nil {
		return "", err
	}

	var epIP string
	var epPort int32
	for _, subset := range endpoint.Subsets {
		for _, addr := range subset.Addresses {
			if addr.TargetRef.Name == pod.Name {
				epIP = addr.IP
			}
		}
		for _, port := range subset.Ports {
			if port.Name == argusdSvcPortName {
				epPort = port.Port
			}
		}
	}
	if epIP == "" || epPort == 0 {
		return "", fmt.Errorf("cannot locate endpoint IP or port on node %v", pod.Spec.NodeName)
	}
	return fmt.Sprintf("%s:%d", epIP, epPort), nil
}

// getHostURLFromSiblingPod constructs a URL from a daemon pod running on the
// same host; it uses the daemon pod's hostIP and hard-coded ArgusD port. The
// pod specified in this function is assumed to be a non-daemon pod. If
// argusdURL was specified to the controller, to connect to an out-of-cluster
// daemon, use this instead.
func (awc *ArgusWatcherController) getHostURLFromSiblingPod(pod *corev1.Pod) (string, error) {
	if awc.argusdURL != "" {
		return awc.argusdURL, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: argusdSelector})
	if err != nil {
		return "", err
	}
	daemonPods, err := awc.podLister.Pods(argusNamespace).List(selector)
	if err != nil {
		return "", err
	}
	for _, daemonPod := range daemonPods {
		if daemonPod.Spec.NodeName == pod.Spec.NodeName {
			hostURL, err := awc.getHostURL(daemonPod)
			if err != nil {
				return "", err
			}
			return hostURL, nil
		}
	}

	return "", fmt.Errorf("cannot locate argusd pod on node %v", pod.Spec.NodeName)
}

// getArgusWatcherSubjects is a helper function that given a ArgusWatcher,
// constructs a list of Subjects to be used when creating a new ArgusD watcher.
func (awc *ArgusWatcherController) getArgusWatcherSubjects(aw *argusv1alpha1.ArgusWatcher) []*pb.ArgusWatcherSubject {
	var subjects []*pb.ArgusWatcherSubject
	for _, s := range aw.Spec.Subjects {
		subjects = append(subjects, &pb.ArgusWatcherSubject{
			Path:      s.Paths,
			Event:     s.Events,
			Ignore:    s.Ignore,
			OnlyDir:   s.OnlyDir,
			Recursive: s.Recursive,
			MaxDepth:  s.MaxDepth,
			Tags:      s.Tags,
		})
	}
	return subjects
}

// getArgusdConnection returns a argusdConnection object given a hostURL.
func (awc *ArgusWatcherController) getArgusdConnection(hostURL string) (*argusdConnection, error) {
	if fc, ok := awc.argusdConnections.Load(hostURL); ok == true {
		return fc.(*argusdConnection), nil
	}
	return nil, fmt.Errorf("could not connect to argusd at hostURL %v", hostURL)
}

// GetWatchStates is a helper function that gets all the current watch states
// from every ArgusD pod running in the clutser. This is used in the
// syncHandler to run exactly once each sync.
func (awc *ArgusWatcherController) getWatchStates() ([][]*pb.ArgusdHandle, error) {
	var watchStates [][]*pb.ArgusdHandle

	// If specifying a argusdURL for a daemon that is located out-of-cluster,
	// we assume a single ArgusD pod in the cluster; get the watch state of
	// this daemon pod only.
	if awc.argusdURL != "" {
		fc, err := awc.getArgusdConnection(awc.argusdURL)
		if err != nil {
			return nil, err
		}
		ws, err := fc.GetWatchState()
		if err != nil {
			return nil, err
		}
		watchStates = append(watchStates, ws)
		return watchStates, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: argusdSelector})
	if err != nil {
		return nil, err
	}
	daemonPods, err := awc.podLister.Pods(argusNamespace).List(selector)
	if err != nil {
		return nil, err
	}
	for _, pod := range daemonPods {
		hostURL, err := awc.getHostURL(pod)
		if err != nil {
			continue
		}
		fc, err := awc.getArgusdConnection(hostURL)
		if err != nil {
			return nil, err
		}
		ws, err := fc.GetWatchState()
		if err != nil {
			continue
		}
		watchStates = append(watchStates, ws)
	}
	return watchStates, nil
}

// isPodInWatchState is a helper function that given a pod and list of watch
// states, find if pod name appears anywhere in the list.
func (awc *ArgusWatcherController) isPodInWatchState(pod *corev1.Pod, watchStates [][]*pb.ArgusdHandle) bool {
	var found bool
	for _, watchState := range watchStates {
		for _, ws := range watchState {
			if pod.Name == ws.PodName {
				found = true
				break
			}
		}
	}
	return found
}

// removePodFromUpdateQueue is a helper function that given a pod name, remove
// it from the update pod queue in order to be marked for a new update in the
// future.
func (awc *ArgusWatcherController) removePodFromUpdateQueue(podName string) {
	updatePodQueue.Delete(podName)
}
