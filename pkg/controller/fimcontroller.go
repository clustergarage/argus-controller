package fimcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

	fimv1alpha1 "clustergarage.io/fim-controller/pkg/apis/fimcontroller/v1alpha1"
	clientset "clustergarage.io/fim-controller/pkg/client/clientset/versioned"
	fimscheme "clustergarage.io/fim-controller/pkg/client/clientset/versioned/scheme"
	informers "clustergarage.io/fim-controller/pkg/client/informers/externalversions/fimcontroller/v1alpha1"
	listers "clustergarage.io/fim-controller/pkg/client/listers/fimcontroller/v1alpha1"
	pb "clustergarage.io/fim-proto/golang"
)

const (
	controllerAgentName = "fimcontroller"

	fimdSvc       = "fimd-svc"
	fimdNamespace = "kube-system"

	// SuccessSynced is used as part of the Event 'reason' when a FimWatcher is synced
	SuccessSynced = "Synced"
	// MessageResourceSynced is the message used for an Event fired when a FimWatcher
	// is synced successfully
	MessageResourceSynced = "FimWatcher synced successfully"

	FimAnnotationKeyPrefix  = "fimcontroller.clustergarage.io/"
	FimWatcherAnnotationKey = FimAnnotationKeyPrefix + "fim-watcher"
	FimdHandleAnnotationKey = FimAnnotationKeyPrefix + "fimd-handle"

	// The number of times we retry updating a FimWatcher's status.
	statusUpdateRetries = 1

	minReadySeconds = 60
)

// Controller is the controller implementation for FimWatcher resources
type FimWatcherController struct {
	// GroupVersionKind indicates the controller type.
	// Different instances of this struct may handle different GVKs.
	// For example, this struct can be used (with adapters) to handle ReplicationController.
	schema.GroupVersionKind

	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// fimclientset is a clientset for our own API group
	fimclientset clientset.Interface

	// A TTLCache of pod creates/deletes each fw expects to see.
	expectations controller.ControllerExpectationsInterface

	// A store of FimWatchers, populated by the shared informer passed to NewFimWatcherController
	fwLister listers.FimWatcherLister
	// fwListerSynced returns true if the pod store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	fwListerSynced cache.InformerSynced

	// A store of pods, populated by the shared informer passed to NewFimWatcherController
	podLister corelisters.PodLister
	// podListerSynced returns true if the pod store has been synced at least once.
	// Added as a member to the struct to allow injection for testing.
	podListerSynced cache.InformerSynced

	svcLister corelisters.ServiceLister

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

// NewFimWatcherController returns a new fim watch controller
func NewFimWatcherController(kubeclientset kubernetes.Interface, fimclientset clientset.Interface,
	fwInformer informers.FimWatcherInformer, podInformer coreinformers.PodInformer,
	svcInformer coreinformers.ServiceInformer) *FimWatcherController {

	// Create event broadcaster
	// Add fimcontroller types to the default Kubernetes Scheme so Events can be
	// logged for fimcontroller types.
	fimscheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	fwc := &FimWatcherController{
		GroupVersionKind: appsv1.SchemeGroupVersion.WithKind("FimWatcher"),
		kubeclientset:    kubeclientset,
		fimclientset:     fimclientset,
		expectations:     controller.NewControllerExpectations(),
		fwLister:         fwInformer.Lister(),
		fwListerSynced:   fwInformer.Informer().HasSynced,
		podLister:        podInformer.Lister(),
		podListerSynced:  podInformer.Informer().HasSynced,
		svcLister:        svcInformer.Lister(),
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "FimWatchers"),
		recorder:         recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when FimWatcher resources change
	fwInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    fwc.enqueueFimWatcher,
		UpdateFunc: fwc.updateFimWatcher,
		DeleteFunc: fwc.enqueueFimWatcher,
	})

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: fwc.addPod,
		// This invokes the FimWatcher for every pod change, eg: host assignment. Though this might seem like
		// overkill the most frequent pod update is status, and the associated FimWatcher will only list from
		// local storage, so it should be ok.
		UpdateFunc: fwc.updatePod,
		DeleteFunc: fwc.deletePod,
	})

	return fwc
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (fwc *FimWatcherController) Run(workers int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer fwc.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting FimWatcher controller")
	defer glog.Info("Shutting down FimWatcher controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, fwc.podListerSynced, fwc.fwListerSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process FimWatcher resources
	for i := 0; i < workers; i++ {
		go wait.Until(fwc.runWorker, time.Second, stopCh)
	}
	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// getPodFimWatchers returns a list of FimWatchers matching the given pod
func (fwc *FimWatcherController) getPodFimWatchers(pod *corev1.Pod) []*fimv1alpha1.FimWatcher {
	if len(pod.Labels) == 0 {
		fmt.Errorf("no FimWatchers found for pod %v because it has no labels", pod.Name)
		return nil
	}

	list, err := fwc.fwLister.FimWatchers(pod.Namespace).List(labels.Everything())
	if err != nil {
		return nil
	}

	var fws []*fimv1alpha1.FimWatcher
	for _, fw := range list {
		if fw.Namespace != pod.Namespace {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(fw.Spec.Selector)
		if err != nil {
			fmt.Errorf("invalid selector: %v", err)
			return nil
		}

		// If a FimWatcher with a nil or empty selector creeps in, it should match nothing, not everything.
		if selector.Empty() || !selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		fws = append(fws, fw)
	}

	if len(fws) == 0 {
		fmt.Errorf("could not find FimWatcher for pod %s in namespace %s with labels: %v", pod.Name, pod.Namespace, pod.Labels)
		return nil
	}

	if len(fws) > 1 {
		// ControllerRef will ensure we don't do anything crazy, but more than one
		// item in this list nevertheless constitutes user error.
		runtime.HandleError(fmt.Errorf("user error! more than one %v is selecting pods with labels: %+v", fwc.Kind, pod.Labels))
	}
	return fws
}

// callback when FimWatcher is updated
func (fwc *FimWatcherController) updateFimWatcher(old, new interface{}) {
	oldFW := old.(*fimv1alpha1.FimWatcher)
	newFW := new.(*fimv1alpha1.FimWatcher)

	subjectsChanged := !reflect.DeepEqual(newFW.Spec.Subjects, oldFW.Spec.Subjects)
	if subjectsChanged {
		// add new fimwatcher definitions
		selector, err := metav1.LabelSelectorAsSelector(newFW.Spec.Selector)
		if err != nil {
			return
		}
		if selectedPods, err := fwc.podLister.Pods(newFW.Namespace).List(selector); err == nil {
			for _, pod := range selectedPods {
				go fwc.updatePodOnceValid(pod, newFW)
			}
		}
	}

	fwc.enqueueFimWatcher(new)
}

// When a pod is created, enqueue the fim watcher that manages it and update its expectations.
func (fwc *FimWatcherController) addPod(obj interface{}) {
	pod := obj.(*corev1.Pod)
	fmt.Println(" [addPod] ", pod.Name)

	if pod.DeletionTimestamp != nil {
		// on a restart of the controller manager, it's possible a new pod shows up in a state that
		// is already pending deletion. Prevent the pod from being a creation observation.
		fwc.deletePod(pod)
		return
	}

	// check if pod already has annotation; no need to queue if so
	if fw, found := pod.GetAnnotations()[FimWatcherAnnotationKey]; found {
		fwKey, err := controller.KeyFunc(fw)
		if err != nil {
			return
		}
		fwc.expectations.CreationObserved(fwKey)
		glog.V(4).Infof("Annotated pod %s found: %#v.", pod.Name, pod)
		fwc.enqueueFimWatcher(fw)
		return
	}

	// Otherwise, it's unannotated. Get a list of all matching FimWatchers and sync
	// them to see if anyone wants to adopt it.
	// DO NOT observe creation because no controller should be waiting for an
	// orphan.
	fws := fwc.getPodFimWatchers(pod)
	if len(fws) == 0 {
		return
	}
	glog.V(4).Infof("Unannotated pod %s found: %#v.", pod.Name, pod)
	for _, fw := range fws {
		fwc.enqueueFimWatcher(fw)
	}
}

// When a pod is updated, figure out what fim watcher(s) manage it and wake them
// up. If the labels of the pod have changed we need to awaken both the old
// and new fim watcher. old and new must be *corev1.Pod types.
func (fwc *FimWatcherController) updatePod(old, new interface{}) {
	newPod := new.(*corev1.Pod)
	oldPod := old.(*corev1.Pod)
	//fmt.Println(" [updatePod] ", oldPod.Name, newPod.Name)

	if newPod.ResourceVersion == oldPod.ResourceVersion {
		// Periodic resync will send update events for all known pods.
		// Two different versions of the same pod will always have different RVs.
		return
	}

	/*
		if fwName, found := newPod.GetAnnotations()[FimWatcherAnnotationKey]; found {
			fw, err := fwc.fwLister.FimWatchers(newPod.Namespace).Get(fwName)
			if err != nil {
				return
			}
			glog.V(4).Infof("Pod %s updated, objectMeta %+v -> %+v.", newPod.Name, oldPod.ObjectMeta, newPod.ObjectMeta)
			//fmt.Printf("Pod %s updated, objectMeta %+v -> %+v.\n", newPod.Name, oldPod.ObjectMeta, newPod.ObjectMeta)
			fwc.enqueueFimWatcher(fw)

			// TODO: MinReadySeconds in the Pod will generate an Available condition to be added in
			// the Pod status which in turn will trigger a requeue of the owning fim watcher thus
			// having its status updated with the newly available subject. For now, we can fake the
			// update by resyncing the controller MinReadySeconds after the it is requeued because
			// a Pod transitioned to Ready.
			// Note that this still suffers from #29229, we are just moving the problem one level
			// "closer" to kubelet (from the deployment to the subject set controller).
			if !podutil.IsPodReady(oldPod) &&
				podutil.IsPodReady(newPod) {
				glog.V(2).Infof("%v %q will be enqueued after %ds for availability check", fwc.Kind, fw.Name, minReadySeconds)
				//fmt.Printf("%v %q will be enqueued after %ds for availability check\n", fwc.Kind, fw.Name, minReadySeconds)
				// Add a second to avoid milliseconds skew in AddAfter.
				// See https://github.com/kubernetes/kubernetes/issues/39785#issuecomment-279959133 for more info.
				fwc.enqueueFimWatcherAfter(fw, (time.Duration(minReadySeconds)*time.Second)+time.Second)
			}
			return
		}
	*/

	labelChanged := !reflect.DeepEqual(newPod.Labels, oldPod.Labels)
	if newPod.DeletionTimestamp != nil {
		// when a pod is deleted gracefully it's deletion timestamp is first modified to reflect a grace period,
		// and after such time has passed, the kubelet actually deletes it from the store. We receive an update
		// for modification of the deletion timestamp and expect an fw to create more watchers asap, not wait
		// until the kubelet actually deletes the pod. This is different from the Phase of a pod changing, because
		// an fw never initiates a phase change, and so is never asleep waiting for the same.
		fwc.deletePod(newPod)
		if labelChanged {
			// we don't need to check the oldPod.DeletionTimestamp because DeletionTimestamp cannot be unset.
			fwc.deletePod(oldPod)
		}
		return
	}
}

// When a pod is deleted, enqueue the replica set that manages the pod and update its expectations.
// obj could be an *v1.Pod, or a DeletionFinalStateUnknown marker item.
func (fwc *FimWatcherController) deletePod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	fmt.Println(" [deletePod] ", pod.Name)

	// When a delete is dropped, the relist will notice a pod in the store not
	// in the list, leading to the insertion of a tombstone object which contains
	// the deleted key/value. Note that this value might be stale. If the pod
	// changed labels the new ReplicaSet will not be woken up till the periodic resync.
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

	if fwName, found := pod.GetAnnotations()[FimWatcherAnnotationKey]; found {
		fw, err := fwc.fwLister.FimWatchers(pod.Namespace).Get(fwName)
		if err != nil {
			return
		}
		fwKey, err := controller.KeyFunc(fw)
		if err != nil {
			return
		}
		glog.V(4).Infof("Annotated pod %s/%s deleted through %v, timestamp %+v: %#v.", pod.Namespace, pod.Name, runtime.GetCaller(), pod.DeletionTimestamp, pod)
		fwc.expectations.DeletionObserved(fwKey)
		fwc.enqueueFimWatcher(fw)
	}
}

// enqueueFimWatcher takes a FimWatcher resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than FimWatcher.
func (fwc *FimWatcherController) enqueueFimWatcher(obj interface{}) {
	key, err := controller.KeyFunc(obj)
	fmt.Println(" [enqueueFimWatcher] ", key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", obj, err))
		return
	}
	fwc.workqueue.AddRateLimited(key)
}

// obj could be an *fimv1alpha1.FimWatcher, or a DeletionFinalStateUnknown marker item.
func (fwc *FimWatcherController) enqueueFimWatcherAfter(obj interface{}, after time.Duration) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		runtime.HandleError(fmt.Errorf("couldn't get key for object %+v: %v", obj, err))
		return
	}
	fwc.workqueue.AddAfter(key, after)
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (fwc *FimWatcherController) runWorker() {
	for fwc.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (fwc *FimWatcherController) processNextWorkItem() bool {
	obj, shutdown := fwc.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer fwc.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer fwc.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			fwc.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// FimWatcher resource to be synced.
		if err := fwc.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		fwc.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// manageObserverPods checks and updates observers for the given FimWatcher.
// It will requeue the fim watcher in case of an error while creating/deleting pods.
func (fwc *FimWatcherController) manageObserverPods(rmPods []*corev1.Pod, addPods []*corev1.Pod, fw *fimv1alpha1.FimWatcher) error {
	fmt.Println("     [manageObserverPods] ", "rm:", len(rmPods), "| add:", len(addPods))

	fwKey, err := controller.KeyFunc(fw)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Couldn't get key for %v %#v: %v", fwc.Kind, fw, err))
		return nil
	}

	if len(rmPods) > 0 {
		fwc.expectations.ExpectDeletions(fwKey, len(rmPods))
		glog.V(2).Infof("Too many subjects for %v %s/%s, need %d, deleting %d", fwc.Kind, fw.Namespace, fw.Name, len(fw.Spec.Subjects), len(rmPods))
	}
	if len(addPods) > 0 {
		fwc.expectations.ExpectCreations(fwKey, len(addPods))
		glog.V(2).Infof("Too few subjects for %v %s/%s, need %d, creating %d", fwc.Kind, fw.Namespace, fw.Name, len(fw.Spec.Subjects), len(addPods))
	}

	var podsToUpdate []*corev1.Pod
	for _, pod := range rmPods {
		if _, found := pod.GetAnnotations()[FimdHandleAnnotationKey]; found {
			cids := getPodContainerIDs(pod)
			if len(cids) > 0 {
				hostURL, err := fwc.getHostURLFromService(pod)
				if err != nil {
					return err
				}
				removeFimdWatcher(hostURL, &pb.FimdConfig{
					HostUid:     pod.Spec.NodeName,
					ContainerId: cids,
				})
			}
		}

		err := updateAnnotations([]string{FimWatcherAnnotationKey, FimdHandleAnnotationKey}, nil, pod)
		if err != nil {
			return err
		}
		podsToUpdate = append(podsToUpdate, pod)
	}
	for _, pod := range addPods {
		go fwc.updatePodOnceValid(pod, fw)

		err := updateAnnotations(nil, map[string]string{FimWatcherAnnotationKey: fw.Name}, pod)
		if err != nil {
			return err
		}
		podsToUpdate = append(podsToUpdate, pod)
	}

	for _, pod := range podsToUpdate {
		updatePodWithRetries(fwc.kubeclientset.CoreV1().Pods(pod.Namespace), fwc.podLister,
			fw.Namespace, pod.Name, func(po *corev1.Pod) error {
				po.Annotations = pod.Annotations
				return nil
			})
	}

	return nil
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the FimWatcher resource
// with the current status of the resource.
func (fwc *FimWatcherController) syncHandler(key string) error {
	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing %v %q (%v)", fwc.Kind, key, time.Since(startTime))
	}()

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	fmt.Println("   [syncHandler] ", namespace, name)

	// Get the FimWatcher resource with this namespace/name
	fw, err := fwc.fwLister.FimWatchers(namespace).Get(name)
	if err != nil {
		// The FimWatcher resource may no longer exist, in which case we stop
		// processing.
		if errorsutil.IsNotFound(err) {
			// @TODO: cleanup: delete annotations from any pods that have them
			runtime.HandleError(fmt.Errorf("FimWatcher '%s' in work queue no longer exists", key))
			return nil
		}
		return err
	}

	fwNeedsSync := fwc.expectations.SatisfiedExpectations(key)
	selector, err := metav1.LabelSelectorAsSelector(fw.Spec.Selector)
	if err != nil {
		runtime.HandleError(fmt.Errorf("Error converting pod selector to selector: %v", err))
		return nil
	}

	// list all pods to include the pods that don't match the fw's selector
	// anymore but has the stale controller ref
	allPods, err := fwc.podLister.Pods(fw.Namespace).List(labels.Everything())
	if err != nil {
		return err
	}
	selectedPods, err := fwc.podLister.Pods(fw.Namespace).List(selector)
	if err != nil {
		return err
	}

	// get the diff between all pods and selected pods
	var filteredPods []*corev1.Pod
	for _, pod := range allPods {
		var found bool
		for _, po := range selectedPods {
			if pod == po {
				found = true
				break
			}
		}
		// if pod is currently being destroyed
		// or no longer found in selected pods
		if pod.DeletionTimestamp != nil || !found {
			filteredPods = append(filteredPods, pod)
		}
	}

	var rmPods []*corev1.Pod
	var addPods []*corev1.Pod
	for _, pod := range filteredPods {
		// if pod is still annotated with observable key
		if value, found := pod.GetAnnotations()[FimWatcherAnnotationKey]; found && value == fw.Name {
			rmPods = append(rmPods, pod)
		}
	}
	for _, pod := range selectedPods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		// if pod is not annotated with observable key
		if _, found := pod.GetAnnotations()[FimWatcherAnnotationKey]; !found {
			addPods = append(addPods, pod)
		}
	}

	var manageSubjectsErr error
	if (fwNeedsSync && fw.DeletionTimestamp == nil) ||
		len(rmPods) > 0 ||
		len(addPods) > 0 {
		manageSubjectsErr = fwc.manageObserverPods(rmPods, addPods, fw)
	}

	fw = fw.DeepCopy()
	newStatus := calculateStatus(fw, selectedPods, manageSubjectsErr)

	// Always updates status as pods come up or die.
	updatedFW, err := updateFimWatcherStatus(fwc.fimclientset.FimcontrollerV1alpha1().FimWatchers(fw.Namespace), fw, newStatus)
	if err != nil {
		// Multiple things could lead to this update failing. Requeuing the replica set ensures
		// Returning an error causes a requeue without forcing a hotloop
		return err
	}
	_, err = fwc.fimclientset.FimcontrollerV1alpha1().FimWatchers(fw.Namespace).Update(updatedFW)
	if err != nil {
		return err
	}

	fwc.recorder.Event(fw, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)

	/*
		if manageSubjectsErr == nil &&
			updatedFW.Status.ObservablePods != len(updatedFW.Spec.Subjects) {
			fwc.enqueueFimWatcherAfter(updatedFW, time.Duration(minReadySeconds)*time.Second)
		}
	*/

	return manageSubjectsErr
}

func (fwc *FimWatcherController) updatePodOnceValid(pod *corev1.Pod, fw *fimv1alpha1.FimWatcher) {
	var cids []string
	var nodeName, hostURL string
	retry.RetryOnConflict(wait.Backoff{
		// @TODO: re-evaluate these values
		Steps:    10,
		Duration: 1 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
	}, func() error {
		var err error
		po, err := fwc.podLister.Pods(fw.Namespace).Get(pod.Name)
		if err != nil {
			return err
		}

		if po.Spec.NodeName == "" || po.Status.HostIP == "" {
			err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
				po.Name, errors.New("host name/ip not available"))
			return err
		}

		for _, ctr := range po.Status.ContainerStatuses {
			if ctr.ContainerID == "" {
				err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
					po.Name, errors.New("pod container id not available"))
				return err
			}
			cids = append(cids, ctr.ContainerID)
		}
		if len(po.Spec.Containers) != len(cids) {
			err = errorsutil.NewConflict(schema.GroupResource{Resource: "pods"},
				po.Name, errors.New("available pod container count does not match ready"))
			return err
		}

		nodeName = po.Spec.NodeName
		hostURL, err = fwc.getHostURLFromService(po)

		return err
	})

	if len(cids) == 0 ||
		nodeName == "" ||
		hostURL == "" {
		return
	}

	if fimdHandle := addFimdWatcher(hostURL, &pb.FimdConfig{
		HostUid:     nodeName,
		ContainerId: cids,
		Subject:     fwc.getFimWatcherSubjects(fw),
	}); fimdHandle != nil {
		handlejson, err := json.Marshal(fimdHandle)
		if err != nil {
			return
		}

		if err := updateAnnotations(nil, map[string]string{
			FimdHandleAnnotationKey: string(handlejson),
		}, pod); err != nil {
			return
		}

		updatePodWithRetries(fwc.kubeclientset.CoreV1().Pods(pod.Namespace), fwc.podLister,
			fw.Namespace, pod.Name, func(po *corev1.Pod) error {
				po.Annotations = pod.Annotations
				return nil
			})
	}
}

func (fwc *FimWatcherController) getHostURLFromService(pod *corev1.Pod) (string, error) {
	svc, err := fwc.svcLister.Services(fimdNamespace).Get(fimdSvc)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	port, err := podutil.FindPort(pod, &svc.Spec.Ports[0])
	if err != nil {
		return "", err
	}
	fmt.Println(svc.Spec.ClusterIP, port)
	return fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, port), nil
	//return "0.0.0.0:50051", nil
}

func (fwc *FimWatcherController) getFimWatcherSubjects(fw *fimv1alpha1.FimWatcher) []*pb.FimWatcherSubject {
	var subjects []*pb.FimWatcherSubject
	for _, s := range fw.Spec.Subjects {
		subjects = append(subjects, &pb.FimWatcherSubject{
			Path:  s.Paths,
			Event: s.Events,
		})
	}
	return subjects
}
