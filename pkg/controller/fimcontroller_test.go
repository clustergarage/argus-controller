package fimcontroller

import (
	//"errors"
	"fmt"
	"io"
	//"math/rand"
	//"net/http/httptest"
	//"net/url"
	"reflect"
	//"strings"
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
	gomock "github.com/golang/mock/gomock"
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
	pb "github.com/clustergarage/fim-proto/golang"
	pbmock "github.com/clustergarage/fim-proto/golang/mock"
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
	podNodeName = "fakenode"
	podHostIP   = "fakehost"
	podIP       = "fakepod"

	epName = "fakeendpoint"
)

var (
	fwMatchedLabel    = map[string]string{"foo": "bar"}
	fwNonMatchedLabel = map[string]string{"foo": "baz"}
	fwDaemonLabel     = map[string]string{"foo": "baz"}

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
	//// Objects from here preloaded into NewSimpleFake.
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

	for _, p := range f.podLister {
		items = append(items, p)
	}
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Replace(items, "")

	items = items[:0]
	for _, p := range f.endpointsLister {
		items = append(items, p)
	}
	f.kubeinformers.Core().V1().Endpoints().Informer().GetIndexer().Replace(items, "")

	items = items[:0]
	for _, p := range f.fwLister {
		items = append(items, p)
	}
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Replace(items, "")
}

/*
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
*/

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

func newPodList(name string, fw *fimv1alpha1.FimWatcher, store cache.Store, count int, status corev1.PodPhase,
	labelMap map[string]string) *corev1.PodList {

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

func mockGetWatchState(ctrl *gomock.Controller, handle *pb.FimdHandle) *FimdConnection {
	stream := pbmock.NewMockFimd_GetWatchStateClient(ctrl)
	stream.EXPECT().Recv().Return(handle, nil)
	stream.EXPECT().Recv().Return(nil, io.EOF)
	client := pbmock.NewMockFimdClient(ctrl)
	client.EXPECT().GetWatchState(gomock.Any(), gomock.Any()).Return(stream, nil)
	return NewFimdConnection(fwHostURL, client)
}

func (f *fixture) runController(fwc *FimWatcherController, fwKey string, expectError bool) {
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

func (f *fixture) expectCreateFimWatcherAction(fw *fimv1alpha1.FimWatcher) {
	f.actions = append(f.actions, core.NewCreateAction(schema.GroupVersionResource{
		Resource: fwResource,
	}, fw.Namespace, fw))
}

func (f *fixture) expectUpdateFimWatcherAction(fw *fimv1alpha1.FimWatcher) {
	f.actions = append(f.actions, core.NewUpdateAction(schema.GroupVersionResource{
		Resource: fwResource,
	}, fw.Namespace, fw))
}

func (f *fixture) expectDeleteFimWatcherAction(fw *fimv1alpha1.FimWatcher) {
	f.actions = append(f.actions, core.NewDeleteAction(schema.GroupVersionResource{
		Resource: fwResource,
	}, fw.Namespace, fw.Name))
}

func (f *fixture) expectUpdatePodAction(pod *corev1.Pod) {
	f.kubeactions = append(f.kubeactions, core.NewUpdateAction(schema.GroupVersionResource{
		Version:  podVersion,
		Resource: podResource,
	}, pod.Namespace, pod))
}

//=============================================================================

// NewFimWatcherController

func TestSyncFimWatcherDoesNothing(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	pod := newPod("bar", fw, corev1.PodRunning, false, false)
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	fwc := f.newFimWatcherController(nil, nil, nil)

	f.expectUpdateFimWatcherStatusAction(fw)
	f.runController(fwc, GetKey(fw, t), false)
}

func TestLocalFimdConnection(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	pod := newPod("bar", fw, corev1.PodRunning, false, false)
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	fc := mockGetWatchState(ctrl, &pb.FimdHandle{})
	fwc := f.newFimWatcherController(nil, nil, fc)

	f.expectUpdateFimWatcherStatusAction(fw)
	f.runController(fwc, GetKey(fw, t), false)
}

// NewFimWatchController w/fimdConnection

// addFimWatcher | runWorker | processNextWorkItem

func TestWatchControllers(t *testing.T) {
	f := newFixture(t)
	fakeWatch := watch.NewFake()
	client := fake.NewSimpleClientset()
	client.PrependWatchReactor("fimwatchers", core.DefaultWatchReactor(fakeWatch, nil))
	fwc := f.newFimWatcherController(nil, client, nil)

	stopCh := make(chan struct{})
	defer close(stopCh)
	f.fiminformers.Start(stopCh)

	var fw fimv1alpha1.FimWatcher
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
		if !apiequality.Semantic.DeepDerivative(fwSpec, fw) {
			t.Errorf("Expected %#v, but got %#v", fw, fwSpec)
		}
		close(received)
		return nil
	}
	// Start only the FimWatch watcher and the workqueue, send a watch event,
	// and make sure it hits the sync method.
	go wait.Until(fwc.runWorker, 10*time.Millisecond, stopCh)

	fw.Name = "foo"
	fakeWatch.Add(&fw)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}
}

// updateFimWatcher

// addPod

func TestWatchPods(t *testing.T) {
	f := newFixture(t)
	fakeWatch := watch.NewFake()
	kubeclient := kubefake.NewSimpleClientset()
	kubeclient.PrependWatchReactor("pods", core.DefaultWatchReactor(fakeWatch, nil))
	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	fwc := f.newFimWatcherController(kubeclient, nil, nil)

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
		if !apiequality.Semantic.DeepDerivative(fwSpec, fw) {
			t.Errorf("\nExpected %#v,\nbut got %#v", fw, fwSpec)
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

	pods := newPodList("bar", fw, nil, 1, corev1.PodRunning, fwMatchedLabel)
	pod := pods.Items[0]
	pod.Status.Phase = corev1.PodFailed
	fakeWatch.Add(&pod)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}
}

func TestAddDaemonPod(t *testing.T) {
	f := newFixture(t)
	fakeWatch := watch.NewFake()
	kubeclient := kubefake.NewSimpleClientset()
	kubeclient.PrependWatchReactor("pods", core.DefaultWatchReactor(fakeWatch, nil))
	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	pod := newPod("bar", fw, corev1.PodRunning, true, false)
	daemon := newDaemonPod("baz", fw)
	ep := newEndpoint(fimdService, daemon)
	f.podLister = append(f.podLister, pod, daemon)
	f.endpointsLister = append(f.endpointsLister, ep)
	f.kubeobjects = append(f.kubeobjects, pod, daemon, ep)
	fwc := f.newFimWatcherController(kubeclient, nil, nil)

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
		if !apiequality.Semantic.DeepDerivative(fwSpec, fw) {
			t.Errorf("\nExpected %#v,\nbut got %#v", fw, fwSpec)
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

	//fakeWatch.Add(daemon)
	fwc.addPod(daemon)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}
}

// updatePod

func TestUpdatePods(t *testing.T) {
	f := newFixture(t)
	fw1 := newFimWatcher("foo", fwMatchedLabel)
	fw2 := *fw1
	fw2.Spec.Selector = &metav1.LabelSelector{MatchLabels: fwNonMatchedLabel}
	fw2.Name = "bar"
	f.fwLister = append(f.fwLister, fw1, &fw2)
	f.objects = append(f.objects, fw1, &fw2)
	fwc := f.newFimWatcherController(nil, nil, nil)

	received := make(chan string)

	fwc.syncHandler = func(key string) error {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			t.Errorf("Error splitting key: %v", err)
		}
		fwSpec, err := fwc.fwLister.FimWatchers(namespace).Get(name)
		if err != nil {
			t.Errorf("Expected to find fim watcher under key %v: %v", key, err)
		}
		received <- fwSpec.Name
		return nil
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	go wait.Until(fwc.runWorker, 10*time.Millisecond, stopCh)

	// case 1: Pod with a ControllerRef
	pod1 := newPodList("bar", fw1, f.kubeinformers.Core().V1().Pods().Informer().GetIndexer(),
		1, corev1.PodRunning, fwMatchedLabel).Items[0]
	pod1.ResourceVersion = "1"
	pod2 := pod1
	pod2.Labels = fwNonMatchedLabel
	pod2.ResourceVersion = "2"
	fwc.updatePod(&pod1, &pod2)
	expected := sets.NewString(fw2.Name)
	for _, name := range expected.List() {
		t.Logf("Expecting update for %+v", name)
		select {
		case got := <-received:
			if !expected.Has(got) {
				t.Errorf("Expected keys %#v got %v", expected, got)
			}
		case <-time.After(wait.ForeverTestTimeout):
			t.Errorf("Expected update notifications for fim watchers")
		}
	}

	/*
		// case 2: Remove ControllerRef (orphan). Expect to sync label-matching FW.
		pod1 = newPod("bar", fw1, corev1.PodRunning, true, false)
		pod1.ResourceVersion = "1"
		pod1.Labels = fwNonMatchedLabel
		pod2 = pod1
		pod2.ResourceVersion = "2"
		fwc.updatePod(&pod1, &pod2)
		expected = sets.NewString(fw2.Name)
		for _, name := range expected.List() {
			t.Logf("Expecting update for %+v", name)
			select {
			case got := <-received:
				if !expected.Has(got) {
					t.Errorf("Expected keys %#v got %v", expected, got)
				}
			case <-time.After(wait.ForeverTestTimeout):
				t.Errorf("Expected update notifications for fim watchers")
			}
		}

		// case 3: Remove ControllerRef (orphan). Expect to sync both former owner and
		// any label-matching FW.
		pod1 = newPod("bar", fw1, corev1.PodRunning, true, false)
		pod1.ResourceVersion = "1"
		pod1.Labels = fwNonMatchedLabel
		pod2 = pod1
		pod2.ResourceVersion = "2"
		fwc.updatePod(&pod1, &pod2)
		expected = sets.NewString(fw1.Name, fw2.Name)
		for _, name := range expected.List() {
			t.Logf("Expecting update for %+v", name)
			select {
			case got := <-received:
				if !expected.Has(got) {
					t.Errorf("Expected keys %#v got %v", expected, got)
				}
			case <-time.After(wait.ForeverTestTimeout):
				t.Errorf("Expected update notifications for fim watchers")
			}
		}

		// case 4: Keep ControllerRef, change labels. Expect to sync owning FW.
		pod1 = newPod("bar", fw1, corev1.PodRunning, true, false)
		pod1.ResourceVersion = "1"
		pod1.Labels = fwMatchedLabel
		pod2 = pod1
		pod2.Labels = fwNonMatchedLabel
		pod2.ResourceVersion = "2"
		fwc.updatePod(&pod1, &pod2)
		expected = sets.NewString(fw2.Name)
		for _, name := range expected.List() {
			t.Logf("Expecting update for %+v", name)
			select {
			case got := <-received:
				if !expected.Has(got) {
					t.Errorf("Expected keys %#v got %v", expected, got)
				}
			case <-time.After(wait.ForeverTestTimeout):
				t.Errorf("Expected update notifications for fim watchers")
			}
		}
	*/
}

// deletePod

func TestDeleteFinalStateUnknown(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	pod := newPod("bar", fw, corev1.PodRunning, false, true)
	fwc := f.newFimWatcherController(nil, nil, nil)

	received := make(chan string)
	fwc.syncHandler = func(key string) error {
		received <- key
		return nil
	}
	// The DeletedFinalStateUnknown object should cause the FimWatcher manager to insert
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
	pod := newPod("bar", fw, corev1.PodRunning, true, false)

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
	f.runController(fwc, GetKey(fw, t), false)
	f.resetActions()

	// Expectations prevents watchers but not an update on status
	fw.Status.ObservablePods = 1
	f.podLister = f.podLister[:0]
	f.kubeobjects = f.kubeobjects[:0]
	f.updateInformers()
	f.expectUpdateFimWatcherStatusAction(fw)
	f.runController(fwc, GetKey(fw, t), false)
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
	//f.runController(fwc, GetKey(fw, t), false)

	// 2 PUT for the FimWatch status during dormancy window.
	fakeHandler.ValidateRequestCount(t, 1)
}
*/

/*
func TestControllerUpdateRequeue(t *testing.T) {
	f := newFixture(t)
	client := fake.NewSimpleClientset()
	client.PrependReactor("update", "fimwatchers", func(action core.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		return true, nil, errors.New("failed to update status")
	})
	fwc := f.newFimWatcherController(nil, client, nil)

	// This server should force a requeue of the controller because it fails to update status.Replicas.
	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(fw)
	fw.Status = fimv1alpha1.FimWatcherStatus{ObservablePods: 2}
	newPodList("bar", fw, f.kubeinformers.Core().V1().Pods().Informer().GetIndexer(), 1, corev1.PodRunning, fwMatchedLabel)

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

/*
func TestControllerUpdateStatusWithFailure(t *testing.T) {
	//f := newFixture(t)
	client := fake.NewSimpleClientset()
	fw := newFimWatcher("foo", map[string]string{"foo": "bar"})
	client.AddReactor("get", "fimwatchers", func(action core.Action) (bool, runtime.Object, error) {
		return true, fw, nil
	})
	client.AddReactor("*", "*", func(action core.Action) (bool, runtime.Object, error) {
		return true, &fimv1alpha1.FimWatcher{}, fmt.Errorf("Fake error")
	})
	//fwc := f.newFimWatcherController(nil, client, nil)

	fakeFWClient := client.FimcontrollerV1alpha1().FimWatchers("foo")
	numObservablePods := int32(10)
	newStatus := fimv1alpha1.FimWatcherStatus{ObservablePods: numObservablePods}
	updateFimWatcherStatus(fakeFWClient, fw, newStatus)
	updates, gets := 0, 0
	for _, a := range client.Actions() {
		if a.GetResource().Resource != "fimwatchers" {
			t.Errorf("Unexpected action %+v", a)
			continue
		}

		switch action := a.(type) {
		case core.GetAction:
			gets++
			// Make sure the get is for the right FimWatcher even though the update failed.
			if action.GetName() != fw.Name {
				t.Errorf("Expected get for FimWatcher %v, got %+v instead", fw.Name, action.GetName())
			}
		case core.UpdateAction:
			updates++
			// Confirm that the update has the right status.Replicas even though the Get
			// returned a FimWatcher with replicas=1.
			if c, ok := action.GetObject().(*fimv1alpha1.FimWatcher); !ok {
				t.Errorf("Expected a FimWatcher as the argument to update, got %T", c)
			} else if c.Status.ObservablePods != numObservablePods {
				t.Errorf("Expected update for FimWatcher to contain observable pods %v, got %v instead",
					numObservablePods, c.Status.ObservablePods)
			}
		default:
			t.Errorf("Unexpected action %+v", a)
			break
		}
	}
	if gets != 1 || updates != 2 {
		t.Errorf("Expected 1 get and 2 updates, got %d gets %d updates", gets, updates)
	}
}
*/

/*
type FakeFWExpectations struct {
	*controller.ControllerExpectations
	satisfied    bool
	expSatisfied func()
}

func (fe FakeFWExpectations) SatisfiedExpectations(controllerKey string) bool {
	fe.expSatisfied()
	return fe.satisfied
}

// TestFWSyncExpectations tests that a pod cannot sneak in between counting active pods
// and checking expectations.
func TestFWSyncExpectations(t *testing.T) {
	f := newFixture(t)
	fwc := f.newFimWatcherController(nil, nil, nil)

	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(fw)
	pods := newPodList("bar", fw, nil, 2, corev1.PodPending, fwMatchedLabel)
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Add(&pods.Items[0])
	postExpectationsPod := pods.Items[1]

	fwc.expectations = controller.NewUIDTrackingControllerExpectations(FakeFWExpectations{
		controller.NewControllerExpectations(), true, func() {
			// If we check active pods before checking expectataions, the
			// FimWatcher will create a new replica because it doesn't see
			// this pod, but has fulfilled its expectations.
			f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Add(&postExpectationsPod)
		},
	})
	f.expectUpdateFimWatcherStatusAction(fw)
	//fwc.syncFimWatcher(GetKey(fw, t))
	f.runController(fwc, GetKey(fw, t), false)
}

func TestDeleteControllerAndExpectations(t *testing.T) {
	f := newFixture(t)
	fwc := f.newFimWatcherController(nil, nil, nil)

	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(fw)

	// This should set expectations for the FimWatcher
	f.expectCreateFimWatcherAction(fw)
	f.expectUpdateFimWatcherStatusAction(fw)
	//fwc.syncFimWatcher(GetKey(fw, t))
	f.runController(fwc, GetKey(fw, t), false)
	f.resetActions()

	// Get the FimWatcher key
	fwKey, err := controller.KeyFunc(fw)
	if err != nil {
		t.Errorf("Couldn't get key for object %#v: %v", fw, err)
	}
	// This is to simulate a concurrent addPod, that has a handle on the expectations
	// as the controller deletes it.
	podExp, exists, err := fwc.expectations.GetExpectations(fwKey)
	if !exists || err != nil {
		t.Errorf("No expectations found for FimWatcher")
	}
	f.fiminformers.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Delete(fw)
	f.expectDeleteFimWatcherAction(fw)
	f.expectUpdateFimWatcherStatusAction(fw)
	//fwc.syncFimWatcher(GetKey(fw, t))
	f.runController(fwc, GetKey(fw, t), false)
	f.resetActions()

	if _, exists, err = fwc.expectations.GetExpectations(fwKey); exists {
		t.Errorf("Found expectations, expected none since the FimWatcher has been deleted.")
	}

	// This should have no effect, since we've deleted the FimWatcher.
	podExp.Add(-1, 0)
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Replace(make([]interface{}, 0), "0")
	f.expectUpdateFimWatcherStatusAction(fw)
	//fwc.syncFimWatcher(GetKey(fw, t))
	f.runController(fwc, GetKey(fw, t), false)
}
*/

/*
func TestDeletionTimestamp(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	pod := newPodList("bar", fw, nil, 1, corev1.PodRunning, fwMatchedLabel).Items[0]
	pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	pod.ResourceVersion = "1"
	fwc := f.newFimWatcherController(nil, nil, nil)

	fwKey, err := controller.KeyFunc(fw)
	if err != nil {
		t.Errorf("Couldn't get key for object %#v: %v", fw, err)
	}
	fwc.expectations.ExpectDeletions(fwKey, []string{controller.PodKey(&pod)})

	// A pod added with a deletion timestamp should decrement deletions, not creations.
	fwc.addPod(&pod)

	queueFW, _ := fwc.workqueue.Get()
	if queueFW != fwKey {
		t.Fatalf("Expected to find key %v in queue, found %v", fwKey, queueFW)
	}
	fwc.workqueue.Done(fwKey)

	podExp, exists, err := fwc.expectations.GetExpectations(fwKey)
	if !exists || err != nil || !podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}

	// An update from no deletion timestamp to having one should be treated
	// as a deletion.
	oldPod := newPodList("baz", fw, nil, 1, corev1.PodPending, fwMatchedLabel).Items[0]
	oldPod.ResourceVersion = "2"
	fwc.expectations.ExpectDeletions(fwKey, []string{controller.PodKey(&pod)})
	fwc.updatePod(&oldPod, &pod)

	queueFW, _ = fwc.workqueue.Get()
	if queueFW != fwKey {
		t.Fatalf("Expected to find key %v in queue, found %v", fwKey, queueFW)
	}
	fwc.workqueue.Done(fwKey)

	podExp, exists, err = fwc.expectations.GetExpectations(fwKey)
	if !exists || err != nil || !podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}

	// An update to the pod (including an update to the deletion timestamp)
	// should not be counted as a second delete.
	secondPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qux",
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
		},
	}
	fwc.expectations.ExpectDeletions(fwKey, []string{controller.PodKey(secondPod)})
	oldPod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	oldPod.ResourceVersion = "2"
	fwc.updatePod(&oldPod, &pod)

	podExp, exists, err = fwc.expectations.GetExpectations(fwKey)
	if !exists || err != nil || podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}

	// A pod with a non-nil deletion timestamp should also be ignored by the
	// delete handler, because it's already been counted in the update.
	fwc.deletePod(&pod)
	podExp, exists, err = fwc.expectations.GetExpectations(fwKey)
	if !exists || err != nil || podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}

	// Deleting the second pod should clear expectations.
	fwc.deletePod(secondPod)

	queueFW, _ = fwc.workqueue.Get()
	if queueFW != fwKey {
		t.Fatalf("Expected to find key %v in queue, found %v", fwKey, queueFW)
	}
	fwc.workqueue.Done(fwKey)

	podExp, exists, err = fwc.expectations.GetExpectations(fwKey)
	if !exists || err != nil || !podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}
}
*/

/*
// shuffle returns a new shuffled list of container controllers.
func shuffle(controllers []*fimv1alpha1.FimWatcher) []*fimv1alpha1.FimWatcher {
	numControllers := len(controllers)
	randIndexes := rand.Perm(numControllers)
	shuffled := make([]*fimv1alpha1.FimWatcher, numControllers)
	for i := 0; i < numControllers; i++ {
		shuffled[i] = controllers[randIndexes[i]]
	}
	return shuffled
}

func TestOverlappingFimWatchers(t *testing.T) {
	f := newFixture(t)
	// Create 10 FimWatchers, shuffled them randomly and insert them into the
	// FimWatcher controller's store.
	// All use the same CreationTimestamp since ControllerRef should be able
	// to handle that.
	timestamp := metav1.Date(2018, time.October, 0, 0, 0, 0, 0, time.Local)
	var controllers []*fimv1alpha1.FimWatcher
	for i := 1; i < 10; i++ {
		fw := newFimWatcher(fmt.Sprintf("fw%d", i), fwMatchedLabel)
		fw.CreationTimestamp = timestamp
		controllers = append(controllers, fw)
	}
	shuffledControllers := shuffle(controllers)
	for i := range shuffledControllers {
		//informers.Apps().V1().ReplicaSets().Informer().GetIndexer().Add(shuffledControllers[j])
		f.fwLister = append(f.fwLister, shuffledControllers[i])
		f.objects = append(f.objects, shuffledControllers[i])
	}
	// Add a pod and make sure only the corresponding FimWatcher is synced.
	// Pick a FW in the middle since the old code used to sort by name if all
	// timestamps were equal.
	fw := controllers[3]
	pods := newPodList("bar", fw, nil, 1, corev1.PodRunning, fwMatchedLabel)
	pod := &pods.Items[0]
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	fwc := f.newFimWatcherController(nil, nil, nil)

	fwKey := GetKey(fw, t)

	fwc.addPod(pod)
	queueFW, _ := fwc.workqueue.Get()
	if queueFW != fwKey {
		t.Fatalf("Expected to find key %v in queue, found %v", fwKey, queueFW)
	}
}
*/

//=============================================================================

// getPodFimWatchers

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

// getPodKeys

func TestGetPodKeys(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher("foo", fwMatchedLabel)
	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)
	pod1 := newPod("bar", fw, corev1.PodRunning, true, true)
	pod2 := newPod("baz", fw, corev1.PodRunning, true, true)
	f.podLister = append(f.podLister, pod1, pod2)
	f.kubeobjects = append(f.kubeobjects, pod1, pod2)
	f.newFimWatcherController(nil, nil, nil)

	tests := []struct {
		name            string
		pods            []*corev1.Pod
		expectedPodKeys []string
	}{{
		"len(pods) = 0 (i.e., pods = nil)",
		[]*corev1.Pod{},
		[]string{},
	}, {
		"len(pods) > 0",
		[]*corev1.Pod{pod1, pod2},
		[]string{"fim/bar", "fim/baz"},
	}}

	for _, test := range tests {
		podKeys := getPodKeys(test.pods)
		if len(podKeys) != len(test.expectedPodKeys) {
			t.Errorf("%s: unexpected keys for pods to delete, expected %v, got %v", test.name, test.expectedPodKeys, podKeys)
		}
		for i := 0; i < len(podKeys); i++ {
			if podKeys[i] != test.expectedPodKeys[i] {
				t.Errorf("%s: unexpected keys for pods to delete, expected %v, got %v", test.name, test.expectedPodKeys, podKeys)
			}
		}
	}
}
