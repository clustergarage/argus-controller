package fimcontroller

import (
	//"errors"
	"fmt"
	//"math/rand"
	//"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	//"sync"
	"testing"
	"time"

	//appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/apimachinery/pkg/util/sets"
	//"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	kubeinformers "k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	//restclient "k8s.io/client-go/rest"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	//utiltesting "k8s.io/client-go/util/testing"
	//"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"
	. "k8s.io/kubernetes/pkg/controller/testutil"
	//"k8s.io/kubernetes/pkg/securitycontext"

	fimv1alpha1 "clustergarage.io/fim-controller/pkg/apis/fimcontroller/v1alpha1"
	fimclientset "clustergarage.io/fim-controller/pkg/client/clientset/versioned"
	"clustergarage.io/fim-controller/pkg/client/clientset/versioned/fake"
	informers "clustergarage.io/fim-controller/pkg/client/informers/externalversions"
	//pb "github.com/clustergarage/fim-proto/golang"
	//pbmock "github.com/clustergarage/fim-proto/golang_mock"
)

const (
	fimdSvcPort = 12345

	fwGroup    = "fimcontroller.clustergarage.io"
	fwVersion  = "v1alpha1"
	fwResource = "fimwatchers"
	fwKind     = "FimWatcher"
	fwName     = "fim-watcher"
	fwHostURL  = "fakeurl:50051"

	podVersion  = "v1"
	podResource = "pods"
	podName     = "fakepod"
	podNodeName = "fakenode"
	podHostIP   = "fakehost"
	podIP       = "fakepod"

	epName = "fakeendpoint"
)

var (
	fwMatchedLabel    = map[string]string{"foo": "bar"}
	fwNonMatchedLabel = map[string]string{"foo": "baz"}

	fwAnnotated = func(fw *fimv1alpha1.FimWatcher) map[string]string {
		return map[string]string{FimWatcherAnnotationKey: fw.Name}
	}
	alwaysReady        = func() bool { return true }
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *kubefake.Clientset
	// Objects to put in the store.
	fwLister        []*fimv1alpha1.FimWatcher
	podLister       []*corev1.Pod
	endpointsLister []*corev1.Endpoints
	// Informer factories.
	kubeinformers kubeinformers.SharedInformerFactory
	fiminformers  informers.SharedInformerFactory
	// Actions expected to happen on the client.
	kubeactions []core.Action
	actions     []core.Action
	// Objects from here preloaded into NewSimpleFake.
	kubeobjects []runtime.Object
	objects     []runtime.Object
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.t = t
	f.kubeobjects = []runtime.Object{}
	f.objects = []runtime.Object{}
	return f
}

func (f *fixture) newFimWatcherController(kubeclient clientset.Interface, client fimclientset.Interface,
	fimdConnection *FimdConnection) *FimWatcherController {

	if kubeclient == nil {
		f.kubeclient = kubefake.NewSimpleClientset(f.kubeobjects...)
		kubeclient = f.kubeclient
	}
	if client == nil {
		f.client = fake.NewSimpleClientset(f.objects...)
		client = f.client
	}

	f.kubeinformers = kubeinformers.NewSharedInformerFactory(kubeclient, controller.NoResyncPeriodFunc())
	f.fiminformers = informers.NewSharedInformerFactory(client, controller.NoResyncPeriodFunc())

	fwc := NewFimWatcherController(kubeclient, client,
		f.fiminformers.Fimcontroller().V1alpha1().FimWatchers(),
		f.kubeinformers.Core().V1().Pods(),
		f.kubeinformers.Core().V1().Endpoints(),
		fimdConnection)

	fwc.fwListerSynced = alwaysReady
	fwc.podListerSynced = alwaysReady
	fwc.endpointsListerSynced = alwaysReady
	fwc.recorder = &record.FakeRecorder{}

	f.updateInformers()
	return fwc
}

func (f *fixture) updateInformers() {
	var items []interface{}
	// update list of pods in indexer
	for _, p := range f.podLister {
		items = append(items, p)
	}
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Replace(items, "")
	// update list of endpoints in indexer
	items = items[:0]
	for _, p := range f.endpointsLister {
		items = append(items, p)
	}
	f.kubeinformers.Core().V1().Endpoints().Informer().GetIndexer().Replace(items, "")
	// update list of fimwatchers in indexer
	items = items[:0]
	for _, p := range f.fwLister {
		items = append(items, p)
	}
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Replace(items, "")
}

func (f *fixture) resetActions() {
	f.actions = f.actions[:0]
	f.kubeactions = f.kubeactions[:0]
}

func skipListerFn(verb string, url url.URL) bool {
	if verb != "GET" {
		return false
	}
	if strings.HasSuffix(url.Path, "/pods") ||
		strings.HasSuffix(url.Path, "/endpoints") ||
		strings.Contains(url.Path, "/fimwatchers") {
		return true
	}
	return false
}

func newFimWatcher(name string, selectorMap map[string]string) *fimv1alpha1.FimWatcher {
	return &fimv1alpha1.FimWatcher{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fimv1alpha1.SchemeGroupVersion.String(),
			Kind:       fwKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: fimNamespace,
		},
		Spec: fimv1alpha1.FimWatcherSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorMap},
			//Subjects: ,
			//LogFormat: ,
		},
	}
}

func newPod(name string, fw *fimv1alpha1.FimWatcher, status corev1.PodPhase, matchLabels bool, fwWatched bool) *corev1.Pod {
	var conditions []corev1.PodCondition
	if status == corev1.PodRunning {
		condition := corev1.PodCondition{
			Type:   corev1.PodReady,
			Status: corev1.ConditionTrue,
		}
		conditions = append(conditions, condition)
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name,
			Namespace: fw.Namespace,
			Labels: func() map[string]string {
				if matchLabels {
					return fwMatchedLabel
				}
				return fwNonMatchedLabel
			}(),
			Annotations: func() map[string]string {
				if fwWatched {
					return fwAnnotated(fw)
				}
				return nil
			}(),
		},
		Spec: corev1.PodSpec{NodeName: podNodeName},
		Status: corev1.PodStatus{
			Phase:      status,
			Conditions: conditions,
			HostIP:     podHostIP,
		},
	}
}

func newPodList(name string, fw *fimv1alpha1.FimWatcher, store cache.Store, count int, status corev1.PodPhase, labelMap map[string]string) *corev1.PodList {
	pods := []corev1.Pod{}
	for i := 0; i < count; i++ {
		pod := newPod(fmt.Sprintf("%s%d", name, i), fw, status, false, false)
		pod.ObjectMeta.Labels = labelMap
		if store != nil {
			store.Add(pod)
		}
		pods = append(pods, *pod)
	}
	return &corev1.PodList{Items: pods}
}

func newDaemonPod(name string, fw *fimv1alpha1.FimWatcher) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: fw.Namespace,
			Labels:    fimdSelector,
		},
		Spec: corev1.PodSpec{NodeName: podNodeName},
		Status: corev1.PodStatus{
			HostIP: podHostIP,
			PodIP:  podIP,
		},
	}
}

func newEndpoint(name string, pod *corev1.Pod) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: fimNamespace,
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				IP:        epName,
				TargetRef: &corev1.ObjectReference{Name: pod.Name},
			}},
			Ports: []corev1.EndpointPort{{
				Name: fimdSvcPortName,
				Port: fimdSvcPort,
			}},
		}},
	}
}

// processSync initiates a sync via processNextWorkItem() to test behavior that
// depends on both functions (such as re-queueing on sync error).
func processSync(fwc *FimWatcherController, key string) error {
	// Save old syncHandler and replace with one that captures the error.
	oldSyncHandler := fwc.syncHandler
	defer func() {
		fwc.syncHandler = oldSyncHandler
	}()

	var syncErr error
	fwc.syncHandler = func(key string) error {
		syncErr = oldSyncHandler(key)
		return syncErr
	}
	fwc.workqueue.Add(key)
	fwc.processNextWorkItem()
	return syncErr
}

func (f *fixture) runController(fwc *FimWatcherController, fwKey string, fimdConnection *FimdConnection, expectError bool) {
	//if startInformers {
	//	stopCh := make(chan struct{})
	//	defer close(stopCh)
	//	kubeInformerFactory.Start(stopCh)
	//	fimInformerFactory.Start(stopCh)
	//}

	err := fwc.syncHandler(fwKey)
	if !expectError && err != nil {
		f.t.Errorf("error syncing fw: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing fw, got nil")
	}

	actions := filterInformerActions(f.client.Actions())
	for i, action := range actions {
		if len(f.actions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(actions)-len(f.actions), actions[i:])
			break
		}
		expectedAction := f.actions[i]
		checkAction(expectedAction, action, f.t)
	}
	if len(f.actions) > len(actions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.actions)-len(actions), f.actions[len(actions):])
	}

	if f.kubeclient == nil {
		return
	}
	kubeactions := filterInformerActions(f.kubeclient.Actions())
	for i, action := range kubeactions {
		if len(f.kubeactions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(kubeactions)-len(f.kubeactions), kubeactions[i:])
			break
		}
		expectedAction := f.kubeactions[i]
		checkAction(expectedAction, action, f.t)
	}
	if len(f.kubeactions) > len(kubeactions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.kubeactions)-len(kubeactions), f.kubeactions[len(kubeactions):])
	}
}

// checkAction verifies that expected and actual actions are equal and both have
// same attached resources
func checkAction(expected, actual core.Action, t *testing.T) {
	if !(expected.Matches(actual.GetVerb(), actual.GetResource().Resource) &&
		actual.GetSubresource() == expected.GetSubresource()) {
		t.Errorf("Expected\n\t%#v\ngot\n\t%#v", expected, actual)
		return
	}
	if reflect.TypeOf(actual) != reflect.TypeOf(expected) {
		t.Errorf("Action has wrong type. Expected: %t. Got: %t", expected, actual)
		return
	}

	switch a := actual.(type) {
	case core.CreateAction:
		e, _ := expected.(core.CreateAction)
		expObject := e.GetObject()
		object := a.GetObject()
		if !reflect.DeepEqual(expObject, object) {
			t.Errorf("Action %s %s has wrong object\nDiff:\n %s",
				a.GetVerb(), a.GetResource().Resource, diff.ObjectGoPrintDiff(expObject, object))
		}
	case core.UpdateAction:
		e, _ := expected.(core.UpdateAction)
		expObject := e.GetObject()
		object := a.GetObject()
		if !reflect.DeepEqual(expObject, object) {
			t.Errorf("Action %s %s has wrong object\nDiff:\n %s",
				a.GetVerb(), a.GetResource().Resource, diff.ObjectGoPrintDiff(expObject, object))
		}
	case core.PatchAction:
		e, _ := expected.(core.PatchAction)
		expPatch := e.GetPatch()
		patch := a.GetPatch()
		if !reflect.DeepEqual(expPatch, patch) {
			t.Errorf("Action %s %s has wrong patch\nDiff:\n %s",
				a.GetVerb(), a.GetResource().Resource, diff.ObjectGoPrintDiff(expPatch, patch))
		}
	}
}

// filterInformerActions filters list and watch actions for testing resources.
// Since list and watch don't change resource state we can filter it to lower
// noise level in our tests.
func filterInformerActions(actions []core.Action) []core.Action {
	ret := []core.Action{}
	for _, action := range actions {
		if len(action.GetNamespace()) == 0 &&
			(action.Matches("list", "fimwatchers") ||
				action.Matches("watch", "fimwatchers") ||
				action.Matches("list", "pods") ||
				action.Matches("watch", "pods") ||
				action.Matches("list", "endpoints") ||
				action.Matches("watch", "endpoints")) {
			continue
		}
		ret = append(ret, action)
	}
	return ret
}

func (f *fixture) expectUpdateFimWatcherStatusAction(fw *fimv1alpha1.FimWatcher) {
	action := core.NewUpdateAction(schema.GroupVersionResource{
		Group:    fwGroup,
		Version:  fwVersion,
		Resource: fwResource,
	}, fw.Namespace, fw)
	// TODO: Until #38113 is merged, we can't use Subresource
	//action.Subresource = "status"
	f.actions = append(f.actions, action)
}

func (f *fixture) expectUpdateFimWatcherAction(fw *fimv1alpha1.FimWatcher) {
	f.actions = append(f.actions, core.NewUpdateAction(schema.GroupVersionResource{
		Resource: fwResource,
	}, fw.Namespace, fw))
}

func (f *fixture) expectUpdatePodAction(pod *corev1.Pod) {
	f.kubeactions = append(f.kubeactions, core.NewUpdateAction(schema.GroupVersionResource{
		Version:  podVersion,
		Resource: podResource,
	}, pod.Namespace, pod))
}

func TestSyncFimWatcherDoesNothing(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher("foo", fwMatchedLabel)
	pod := newPod(podName, fw, corev1.PodRunning, false, false)

	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	fwc := f.newFimWatcherController(nil, nil, nil)

	f.expectUpdateFimWatcherStatusAction(fw)
	f.runController(fwc, GetKey(fw, t), nil, false)
}

// related: deletePod
func TestDeleteFinalStateUnknown(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher("foo", fwMatchedLabel)
	pod := newPod(podName, fw, corev1.PodRunning, false, true)

	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	fwc := f.newFimWatcherController(nil, nil, nil)

	received := make(chan string)
	fwc.syncHandler = func(key string) error {
		received <- key
		return nil
	}
	// The DeletedFinalStateUnknown object should cause the ReplicaSet manager to insert
	// the controller matching the selectors of the deleted pod into the work queue.
	fwc.deletePod(cache.DeletedFinalStateUnknown{
		Key: "foo",
		Obj: pod,
	})
	go fwc.runWorker()

	expected := GetKey(fw, t)
	select {
	case key := <-received:
		if key != expected {
			t.Errorf("Unexpected sync all for FimWatchers %v, expected %v", key, expected)
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("Processing DeleteFinalStateUnknown took longer than expected")
	}
}

/*
func TestSyncFimWatcherDormancy(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher("foo", fwMatchedLabel)
	pod := newPod(podName, fw, corev1.PodRunning, true, false)

	// Setup a test server so we can lie about the current state of pods
	fakeHandler := utiltesting.FakeHandler{
		StatusCode:    200,
		ResponseBody:  "{}",
		SkipRequestFn: skipListerFn,
		T:             t,
	}
	testServer := httptest.NewServer(&fakeHandler)
	defer testServer.Close()

	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	fwc := f.newFimWatcherController(clientset.NewForConfigOrDie(&restclient.Config{
		Host: testServer.URL,
		ContentConfig: restclient.ContentConfig{
			GroupVersion: &schema.GroupVersion{
				Group:   "",
				Version: "v1",
			},
		},
	}), nil)

	// Creates a fimwatch and sets expectations
	fw.Status.ObservablePods = 1
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	f.updateInformers()
	f.expectUpdateFimWatcherStatusAction(fw)
	f.runController(fwc, GetKey(fw, t), nil, false)
	f.resetActions()

	// Expectations prevents watchers but not an update on status
	fw.Status.ObservablePods = 1
	f.podLister = f.podLister[:0]
	f.kubeobjects = f.kubeobjects[:0]
	f.updateInformers()
	f.expectUpdateFimWatcherStatusAction(fw)
	f.runController(fwc, GetKey(fw, t), nil, false)
	f.resetActions()

	//// Get the key for the controller
	//fwKey, err := controller.KeyFunc(fw)
	//if err != nil {
	//	t.Errorf("Couldn't get key for object %#v: %v", fw, err)
	//}
	//// Lowering expectations should lead to a sync that creates a replica, however the
	//// fakePodControl error will prevent this, leaving expectations at 0, 0
	//fwc.expectations.CreationObserved(fwKey)
	//f.podLister = append(f.podLister, pod)
	//f.kubeobjects = append(f.kubeobjects, pod)
	//f.updateInformers()
	//f.expectUpdateFimWatcherStatusAction(fw)
	//f.runController(fwc, GetKey(fw, t), nil, false)

	// 2 PUT for the FimWatch status during dormancy window.
	fakeHandler.ValidateRequestCount(t, 1)
}
*/

func TestPodControllerLookup(t *testing.T) {
	f := newFixture(t)
	fwc := f.newFimWatcherController(nil, nil, nil)

	testCases := []struct {
		inFWs       []*fimv1alpha1.FimWatcher
		pod         *corev1.Pod
		outFWName   string
		expectError bool
	}{{
		// Pods without labels don't match any FimWatchers.
		inFWs: []*fimv1alpha1.FimWatcher{{
			ObjectMeta: metav1.ObjectMeta{Name: "lorem"},
		}},
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: metav1.NamespaceAll,
			},
		},
		outFWName: "",
	}, {
		// Matching labels, not namespace.
		inFWs: []*fimv1alpha1.FimWatcher{{
			ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			Spec: fimv1alpha1.FimWatcherSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"foo": "bar"},
				},
			},
		}},
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "lorem",
				Namespace: "ipsum",
				Labels:    map[string]string{"foo": "bar"},
			}},
		outFWName: "",
	}, {
		// Matching namespace and labels returns the key to the FimWatcher, not the FimWatcher name.
		inFWs: []*fimv1alpha1.FimWatcher{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "ipsum",
			},
			Spec: fimv1alpha1.FimWatcherSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"foo": "bar"},
				},
			},
		}},
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "lorem",
				Namespace: "ipsum",
				Labels:    map[string]string{"foo": "bar"},
			},
		},
		outFWName: "bar",
	}, {
		// Pod with invalid labelSelector causes an error.
		inFWs: []*fimv1alpha1.FimWatcher{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "ipsum",
			},
			Spec: fimv1alpha1.FimWatcherSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"/foo": ""},
				},
			},
		}},
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "lorem",
				Namespace: "ipsum",
				Labels:    map[string]string{"foo": "bar"},
			},
		},
		outFWName:   "",
		expectError: true,
	}, {
		// More than one FimWatcher selected for a pod creates an error.
		inFWs: []*fimv1alpha1.FimWatcher{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "ipsum",
			},
			Spec: fimv1alpha1.FimWatcherSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"foo": "bar"},
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name:      "baz",
				Namespace: "ipsum",
			},
			Spec: fimv1alpha1.FimWatcherSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"foo": "bar"},
				},
			},
		}},
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "lorem",
				Namespace: "ipsum",
				Labels:    map[string]string{"foo": "bar"},
			},
		},
		outFWName:   "",
		expectError: true,
	}}

	for _, tc := range testCases {
		for _, fw := range tc.inFWs {
			f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(fw)
		}
		if fws := fwc.getPodFimWatchers(tc.pod); fws != nil {
			if len(fws) > 1 && tc.expectError {
				continue
			} else if len(fws) != 1 {
				t.Errorf("len(fws) = %v, want %v", len(fws), 1)
				continue
			}
			fw := fws[0]
			if tc.outFWName != fw.Name {
				t.Errorf("Got fim watcher %+v expected %+v", fw.Name, tc.outFWName)
			}
		} else if tc.outFWName != "" {
			t.Errorf("Expected a fim watcher %v pod %v, found none", tc.outFWName, tc.pod.Name)
		}
	}
}

func TestWatchControllers(t *testing.T) {
	f := newFixture(t)
	// @TODO: put this as optional config passed to newFimWatcherController?
	fakeWatch := watch.NewFake()
	client := fake.NewSimpleClientset()
	client.PrependWatchReactor("fimwatchers", core.DefaultWatchReactor(fakeWatch, nil))
	fwc := f.newFimWatcherController(nil, client, nil)

	stopCh := make(chan struct{})
	defer close(stopCh)
	f.fiminformers.Start(stopCh)

	var testFWSpec fimv1alpha1.FimWatcher
	received := make(chan string)

	// The update sent through the fakeWatcher should make its way into the workqueue,
	// and eventually into the syncHandler. The handler validates the received controller
	// and closes the received channel to indicate that the test can finish.
	fwc.syncHandler = func(key string) error {
		obj, exists, err := f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().GetByKey(key)
		if !exists || err != nil {
			t.Errorf("Expected to find fim watcher under key %v", key)
		}
		fwSpec := *obj.(*fimv1alpha1.FimWatcher)
		if !apiequality.Semantic.DeepDerivative(fwSpec, testFWSpec) {
			t.Errorf("Expected %#v, but got %#v", testFWSpec, fwSpec)
		}
		close(received)
		return nil
	}
	// Start only the FimWatch watcher and the workqueue, send a watch event,
	// and make sure it hits the sync method.
	go wait.Until(fwc.runWorker, 10*time.Millisecond, stopCh)

	testFWSpec.Name = "foo"
	fakeWatch.Add(&testFWSpec)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}
}

func TestWatchPods(t *testing.T) {
	f := newFixture(t)
	fakeWatch := watch.NewFake()
	kubeclient := kubefake.NewSimpleClientset()
	kubeclient.PrependWatchReactor("pods", core.DefaultWatchReactor(fakeWatch, nil))
	fwc := f.newFimWatcherController(kubeclient, nil, nil)

	// Put one FimWatcher into the shared informer.
	labelMap := map[string]string{"foo": "bar"}
	testFWSpec := newFimWatcher("foo", labelMap)
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(testFWSpec)

	received := make(chan string)
	// The pod update sent through the fakeWatcher should figure out the managing FimWatcher and
	// send it into the syncHandler.
	fwc.syncHandler = func(key string) error {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			t.Errorf("Error splitting key: %v", err)
		}
		fwSpec, err := fwc.fwLister.FimWatchers(namespace).Get(name)
		if err != nil {
			t.Errorf("Expected to find fim watcher under key %v: %v", key, err)
		}
		if !apiequality.Semantic.DeepDerivative(fwSpec, testFWSpec) {
			t.Errorf("\nExpected %#v,\nbut got %#v", testFWSpec, fwSpec)
		}
		close(received)
		return nil
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	// Start only the pod watcher and the workqueue, send a watch event,
	// and make sure it hits the sync method for the right FimWatcher.
	go f.kubeinformers.Core().V1().Pods().Informer().Run(stopCh)
	go fwc.Run(1, stopCh)

	pods := newPodList("bar", testFWSpec, nil, 1, corev1.PodRunning, labelMap)
	testPod := pods.Items[0]
	testPod.Status.Phase = corev1.PodFailed
	fakeWatch.Add(&testPod)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}
}

func TestUpdatePods(t *testing.T) {
	f := newFixture(t)
	fwc := f.newFimWatcherController(nil, nil, nil)

	received := make(chan string)

	fwc.syncHandler = func(key string) error {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		fmt.Println(" 1.) ", namespace, name, err)
		if err != nil {
			t.Errorf("Error splitting key: %v", err)
		}
		fwSpec, err := fwc.fwLister.FimWatchers(namespace).Get(name)
		fmt.Println(" 2.) ", fwSpec, err)
		if err != nil {
			t.Errorf("Expected to find replica set under key %v: %v", key, err)
		}
		received <- fwSpec.Name
		fmt.Println(" 3.) ", fwSpec.Name)
		return nil
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	go wait.Until(fwc.runWorker, 10*time.Millisecond, stopCh)

	// Put 2 FimWatchers and one pod into the informers
	labelMap1 := map[string]string{"foo": "bar"}
	testFWSpec1 := newFimWatcher("foo", labelMap1)
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(testFWSpec1)
	testFWSpec2 := *testFWSpec1
	labelMap2 := map[string]string{"bar": "foo"}
	testFWSpec2.Spec.Selector = &metav1.LabelSelector{MatchLabels: labelMap2}
	testFWSpec2.Name = "barfoo"
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(&testFWSpec2)

	// case 1: @TODO document this
	pod1 := newPodList("bar", testFWSpec1, f.kubeinformers.Core().V1().Pods().Informer().GetIndexer(),
		1, corev1.PodRunning, labelMap1).Items[0]
	pod1.ResourceVersion = "1"
	pod2 := pod1
	pod2.Labels = labelMap2
	pod2.ResourceVersion = "2"
	fwc.updatePod(&pod1, &pod2)
	expected := sets.NewString(testFWSpec2.Name)
	for _, name := range expected.List() {
		t.Logf("Expecting update for %+v", name)
		select {
		case got := <-received:
			fmt.Println(" ### ", expected, got)
			if !expected.Has(got) {
				t.Errorf("Expected keys %#v got %v", expected, got)
			}
		case <-time.After(wait.ForeverTestTimeout):
			t.Errorf("Expected update notifications for replica sets")
		}
	}

	/*
		// case 2: Remove ControllerRef (orphan). Expect to sync label-matching FW.
		pod1 = newPod("pod", testFWSpec1, corev1.PodRunning, true, false)
		pod1.ResourceVersion = "1"
		pod1.Labels = labelMap2
		pod2 = pod1
		pod2.ResourceVersion = "2"
		fwc.updatePod(&pod1, &pod2)
		expected = sets.NewString(testFWSpec2.Name)
		for _, name := range expected.List() {
			t.Logf("Expecting update for %+v", name)
			select {
			case got := <-received:
				if !expected.Has(got) {
					t.Errorf("Expected keys %#v got %v", expected, got)
				}
			case <-time.After(wait.ForeverTestTimeout):
				t.Errorf("Expected update notifications for replica sets")
			}
		}

		// case 3: Remove ControllerRef (orphan). Expect to sync both former owner and
		// any label-matching FW.
		pod1 = newPod("pod", testFWSpec1, corev1.PodRunning, true, false)
		pod1.ResourceVersion = "1"
		pod1.Labels = labelMap2
		pod2 = pod1
		pod2.ResourceVersion = "2"
		fwc.updatePod(&pod1, &pod2)
		expected = sets.NewString(testFWSpec1.Name, testFWSpec2.Name)
		for _, name := range expected.List() {
			t.Logf("Expecting update for %+v", name)
			select {
			case got := <-received:
				if !expected.Has(got) {
					t.Errorf("Expected keys %#v got %v", expected, got)
				}
			case <-time.After(wait.ForeverTestTimeout):
				t.Errorf("Expected update notifications for replica sets")
			}
		}

		// case 4: Keep ControllerRef, change labels. Expect to sync owning FW.
		pod1 = newPod("pod", testFWSpec1, corev1.PodRunning, true, false)
		pod1.ResourceVersion = "1"
		pod1.Labels = labelMap1
		pod2 = pod1
		pod2.Labels = labelMap2
		pod2.ResourceVersion = "2"
		fwc.updatePod(&pod1, &pod2)
		expected = sets.NewString(testFWSpec2.Name)
		for _, name := range expected.List() {
			t.Logf("Expecting update for %+v", name)
			select {
			case got := <-received:
				if !expected.Has(got) {
					t.Errorf("Expected keys %#v got %v", expected, got)
				}
			case <-time.After(wait.ForeverTestTimeout):
				t.Errorf("Expected update notifications for replica sets")
			}
		}
	*/
}

/*
func TestControllerUpdateRequeue(t *testing.T) {
	//f := newFixture(t)
	// This server should force a requeue of the controller because it fails to update status.Replicas.
	labelMap := map[string]string{"foo": "bar"}
	fw := newFimWatcher("foo", labelMap)

	client := fake.NewSimpleClientset(fw)
	client.PrependReactor("update", "fimwatches",
		func(action core.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() != "status" {
				return false, nil, nil
			}
			return true, nil, errors.New("failed to update status")
		})
	kubeclient := kubefake.NewSimpleClientset()
	kubeinformers := kubeinformers.NewSharedInformerFactory(kubeclient, controller.NoResyncPeriodFunc())
	fiminformers := informers.NewSharedInformerFactory(client, controller.NoResyncPeriodFunc())
	fwc := NewFimWatcherController(kubeclient, client,
		fiminformers.Fimcontroller().V1alpha1().FimWatchers(),
		kubeinformers.Core().V1().Pods(),
		kubeinformers.Core().V1().Endpoints(),
		nil)

	fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(fw)
	fw.Status = fimv1alpha1.FimWatcherStatus{ObservablePods: 2}
	newPodList(kubeinformers.Core().V1().Pods().Informer().GetIndexer(), 1, corev1.PodRunning, labelMap, fw, "pod")

	// Enqueue once. Then process it. Disable rate-limiting for this.
	fwc.workqueue = workqueue.NewRateLimitingQueue(workqueue.NewMaxOfRateLimiter())
	fwc.enqueueFimWatcher(fw)
	fwc.processNextWorkItem()
	// It should have been requeued.
	if got, want := fwc.workqueue.Len(), 1; got != want {
		t.Errorf("queue.Len() = %v, want %v", got, want)
	}
}
*/
