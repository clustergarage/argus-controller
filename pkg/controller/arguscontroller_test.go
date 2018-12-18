package arguscontroller

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"reflect"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	kubeinformers "k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/controller"
	. "k8s.io/kubernetes/pkg/controller/testutil"

	argusv1alpha1 "clustergarage.io/argus-controller/pkg/apis/arguscontroller/v1alpha1"
	argusclientset "clustergarage.io/argus-controller/pkg/client/clientset/versioned"
	"clustergarage.io/argus-controller/pkg/client/clientset/versioned/fake"
	informers "clustergarage.io/argus-controller/pkg/client/informers/externalversions"
	pb "github.com/clustergarage/argus-proto/golang"
	pbmock "github.com/clustergarage/argus-proto/golang/mock"
)

const (
	argusdSvcPort = 12345

	awGroup    = "arguscontroller.clustergarage.io"
	awVersion  = "v1alpha1"
	awResource = "arguswatchers"
	awKind     = "ArgusWatcher"
	awHostURL  = "fakeurl:50051"

	podVersion  = "v1"
	podResource = "pods"
	podNodeName = "fakenode"
	podHostIP   = "fakehost"
	podIP       = "fakepod"

	epName = "fakeendpoint"

	interval = 100 * time.Millisecond
	timeout  = 60 * time.Second
)

var (
	awMatchedLabel    = map[string]string{"foo": "bar"}
	awNonMatchedLabel = map[string]string{"foo": "baz"}

	awAnnotated = func(aw *argusv1alpha1.ArgusWatcher) map[string]string {
		return map[string]string{ArgusWatcherAnnotationKey: aw.Name}
	}
	alwaysReady        = func() bool { return true }
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *kubefake.Clientset
	// Objects to put in the store.
	awLister        []*argusv1alpha1.ArgusWatcher
	podLister       []*corev1.Pod
	endpointsLister []*corev1.Endpoints
	// Informer factories.
	kubeinformers  kubeinformers.SharedInformerFactory
	argusinformers informers.SharedInformerFactory
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

func (f *fixture) newArgusWatcherController(kubeclient clientset.Interface, client argusclientset.Interface,
	argusdConnection *argusdConnection) *ArgusWatcherController {

	if kubeclient == nil {
		kubeclient = kubefake.NewSimpleClientset(f.kubeobjects...)
	}
	f.kubeclient = kubeclient.(*kubefake.Clientset)
	if client == nil {
		client = fake.NewSimpleClientset(f.objects...)
	}
	f.client = client.(*fake.Clientset)

	f.kubeinformers = kubeinformers.NewSharedInformerFactory(kubeclient, controller.NoResyncPeriodFunc())
	f.argusinformers = informers.NewSharedInformerFactory(client, controller.NoResyncPeriodFunc())

	awc := NewArgusWatcherController(kubeclient, client,
		f.argusinformers.Arguscontroller().V1alpha1().ArgusWatchers(),
		f.kubeinformers.Core().V1().Pods(),
		f.kubeinformers.Core().V1().Endpoints(),
		argusdConnection)

	awc.awListerSynced = alwaysReady
	awc.podListerSynced = alwaysReady
	awc.endpointsListerSynced = alwaysReady
	awc.recorder = &record.FakeRecorder{}
	awc.backoff = retry.DefaultBackoff

	for _, pod := range f.podLister {
		f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Add(pod)
	}
	for _, ep := range f.endpointsLister {
		f.kubeinformers.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
	}
	for _, aw := range f.awLister {
		f.argusinformers.Arguscontroller().V1alpha1().ArgusWatchers().Informer().GetIndexer().Add(aw)
	}
	return awc
}

func (f *fixture) waitForPodExpectationFulfillment(awc *ArgusWatcherController, awKey string, pod *corev1.Pod) {
	// Wait for sync, CreationObserved via RetryOnConflict loop.
	if err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		_, err := awc.kubeclientset.CoreV1().Pods(pod.Namespace).Get(pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		podExp, _, err := awc.expectations.GetExpectations(awKey)
		if podExp == nil {
			return false, err
		}
		return podExp.Fulfilled(), err
	}); err != nil {
		f.t.Errorf("No expectations found for ArgusWatcher")
	}
}

func newArgusWatcher(name string, selectorMap map[string]string) *argusv1alpha1.ArgusWatcher {
	return &argusv1alpha1.ArgusWatcher{
		TypeMeta: metav1.TypeMeta{
			APIVersion: argusv1alpha1.SchemeGroupVersion.String(),
			Kind:       awKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: argusNamespace,
		},
		Spec: argusv1alpha1.ArgusWatcherSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorMap},
		},
	}
}

func newPod(name string, aw *argusv1alpha1.ArgusWatcher, status corev1.PodPhase, matchLabels bool, awWatched bool) *corev1.Pod {
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
			Namespace: aw.Namespace,
			Labels: func() map[string]string {
				if matchLabels {
					return awMatchedLabel
				}
				return awNonMatchedLabel
			}(),
			Annotations: func() map[string]string {
				if awWatched {
					return awAnnotated(aw)
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

func newPodList(name string, aw *argusv1alpha1.ArgusWatcher, store cache.Store, count int, status corev1.PodPhase,
	labelMap map[string]string, awWatched bool) *corev1.PodList {

	pods := []corev1.Pod{}
	for i := 0; i < count; i++ {
		pod := newPod(fmt.Sprintf("%s%d", name, i), aw, status, false, awWatched)
		pod.ObjectMeta.Labels = labelMap
		if store != nil {
			store.Add(pod)
		}
		pods = append(pods, *pod)
	}
	return &corev1.PodList{Items: pods}
}

func newDaemonPod(name string, aw *argusv1alpha1.ArgusWatcher) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: aw.Namespace,
			Labels:    argusdSelector,
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
			Namespace: argusNamespace,
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				IP:        epName,
				TargetRef: &corev1.ObjectReference{Name: pod.Name},
			}},
			Ports: []corev1.EndpointPort{{
				Name: argusdSvcPortName,
				Port: argusdSvcPort,
			}},
		}},
	}
}

func newMockArgusdClient(ctrl *gomock.Controller) *argusdConnection {
	client := pbmock.NewMockArgusdClient(ctrl)
	conn, _ := NewArgusdConnection(awHostURL, grpc.WithInsecure(), client)
	return conn
}

func stubGetWatchState(ctrl *gomock.Controller, conn *argusdConnection, ret *pb.ArgusdHandle) {
	var client *pbmock.MockArgusdClient
	client = conn.client.(*pbmock.MockArgusdClient)
	stream := pbmock.NewMockArgusd_GetWatchStateClient(ctrl)
	stream.EXPECT().Recv().Return(ret, nil)
	stream.EXPECT().Recv().Return(nil, io.EOF)
	client.EXPECT().GetWatchState(gomock.Any(), gomock.Any()).Return(stream, nil)
}

func stubCreateWatch(ctrl *gomock.Controller, conn *argusdConnection, ret *pb.ArgusdHandle) {
	var client *pbmock.MockArgusdClient
	client = conn.client.(*pbmock.MockArgusdClient)
	client.EXPECT().CreateWatch(gomock.Any(), gomock.Any()).Return(ret, nil)
}

func stubDestroyWatch(ctrl *gomock.Controller, conn *argusdConnection) {
	var client *pbmock.MockArgusdClient
	client = conn.client.(*pbmock.MockArgusdClient)
	client.EXPECT().DestroyWatch(gomock.Any(), gomock.Any()).Return(&pb.Empty{}, nil)
}

func (f *fixture) runController(awc *ArgusWatcherController, awKey string, expectError bool) {
	err := awc.syncHandler(awKey)
	if !expectError && err != nil {
		f.t.Errorf("error syncing aw: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing aw, got nil")
	}

	f.verifyActions()
	if f.kubeclient == nil {
		return
	}
	f.verifyKubeActions()
}

func (f *fixture) verifyActions() {
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
}

func (f *fixture) verifyKubeActions() {
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

// checkAction verifies that expected and actual actions are equal and both
// have same attached resources.
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
			(action.Matches("list", "arguswatchers") ||
				action.Matches("watch", "arguswatchers") ||
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

func (f *fixture) syncHandlerSendArgusWatcherName(awc *ArgusWatcherController, aw *argusv1alpha1.ArgusWatcher,
	received chan string) func(key string) error {

	return func(key string) error {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			f.t.Errorf("Error splitting key: %v", err)
		}
		awSpec, err := awc.awLister.ArgusWatchers(namespace).Get(name)
		if err != nil {
			f.t.Errorf("Expected to find argus watcher under key %v: %v", key, err)
		}
		received <- awSpec.Name
		return nil
	}
}

func (f *fixture) syncHandlerCheckArgusWatcherSynced(awc *ArgusWatcherController, aw *argusv1alpha1.ArgusWatcher,
	received chan string) func(key string) error {

	return func(key string) error {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			f.t.Errorf("Error splitting key: %v", err)
		}
		awSpec, err := awc.awLister.ArgusWatchers(namespace).Get(name)
		if err != nil {
			f.t.Errorf("Expected to find argus watcher under key %v: %v", key, err)
		}
		if !apiequality.Semantic.DeepDerivative(awSpec, aw) {
			f.t.Errorf("\nExpected %#v,\nbut got %#v", aw, awSpec)
		}
		close(received)
		return nil
	}
}

func (f *fixture) expectUpdateArgusWatcherStatusAction(aw *argusv1alpha1.ArgusWatcher) {
	action := core.NewUpdateAction(schema.GroupVersionResource{
		Group:    awGroup,
		Version:  awVersion,
		Resource: awResource,
	}, aw.Namespace, aw)
	action.Subresource = "status"
	f.actions = append(f.actions, action)
}

func (f *fixture) expectGetArgusWatcherAction(aw *argusv1alpha1.ArgusWatcher) {
	f.actions = append(f.actions, core.NewGetAction(schema.GroupVersionResource{
		Group:    awGroup,
		Resource: awResource,
		Version:  awVersion,
	}, aw.Namespace, aw.Name))
}

func (f *fixture) expectUpdateArgusWatcherAction(aw *argusv1alpha1.ArgusWatcher) {
	action := core.NewUpdateAction(schema.GroupVersionResource{
		Group:    awGroup,
		Resource: awResource,
		Version:  awVersion,
	}, aw.Namespace, aw)
	action.Subresource = "status"
	f.actions = append(f.actions, action)
}

type FakeAWExpectations struct {
	*controller.ControllerExpectations
	satisfied    bool
	expSatisfied func()
}

func (fe FakeAWExpectations) SatisfiedExpectations(controllerKey string) bool {
	fe.expSatisfied()
	return fe.satisfied
}

// shuffle returns a new shuffled list of container controllers.
func shuffle(controllers []*argusv1alpha1.ArgusWatcher) []*argusv1alpha1.ArgusWatcher {
	numControllers := len(controllers)
	randIndexes := rand.Perm(numControllers)
	shuffled := make([]*argusv1alpha1.ArgusWatcher, numControllers)
	for i := 0; i < numControllers; i++ {
		shuffled[i] = controllers[randIndexes[i]]
	}
	return shuffled
}

func TestSyncArgusWatcherDoesNothing(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod := newPod("bar", aw, corev1.PodRunning, false, false)
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	awc := f.newArgusWatcherController(nil, nil, nil)

	f.runController(awc, GetKey(aw, t), false)
}

func TestLocalargusdConnection(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod := newPod("bar", aw, corev1.PodRunning, false, false)
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	fc := newMockArgusdClient(ctrl)
	stubGetWatchState(ctrl, fc, &pb.ArgusdHandle{})
	awc := f.newArgusWatcherController(nil, nil, fc)

	f.runController(awc, GetKey(aw, t), false)
}

func TestWatchControllers(t *testing.T) {
	f := newFixture(t)
	fakeWatch := watch.NewFake()
	client := fake.NewSimpleClientset()
	client.PrependWatchReactor("arguswatchers", core.DefaultWatchReactor(fakeWatch, nil))
	awc := f.newArgusWatcherController(nil, client, nil)

	stopCh := make(chan struct{})
	defer close(stopCh)
	f.argusinformers.Start(stopCh)

	var aw argusv1alpha1.ArgusWatcher
	received := make(chan string)
	// The update sent through the fakeWatcher should make its way into the
	// workqueue, and eventually into the syncHandler. The handler validates
	// the received controller and closes the received channel to indicate that
	// the test can finish.
	awc.syncHandler = func(key string) error {
		obj, exists, err := f.argusinformers.Arguscontroller().V1alpha1().ArgusWatchers().Informer().GetIndexer().GetByKey(key)
		if !exists || err != nil {
			t.Errorf("Expected to find argus watcher under key %v", key)
		}
		awSpec := *obj.(*argusv1alpha1.ArgusWatcher)
		if !apiequality.Semantic.DeepDerivative(awSpec, aw) {
			t.Errorf("Expected %#v, but got %#v", aw, awSpec)
		}
		close(received)
		return nil
	}
	// Start only the ArgusWatch watcher and the workqueue, send a watch event,
	// and make sure it hits the sync method.
	go wait.Until(awc.runWorker, 10*time.Millisecond, stopCh)

	aw.Name = "foo"
	fakeWatch.Add(&aw)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}
}

func TestUpdateControllers(t *testing.T) {
	f := newFixture(t)
	aw1 := newArgusWatcher("foo", awMatchedLabel)
	aw2 := *aw1
	aw2.Name = "bar"
	f.awLister = append(f.awLister, aw1, &aw2)
	f.objects = append(f.objects, aw1, &aw2)
	pod := newPod("bar", aw1, corev1.PodRunning, true, false)
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	awc := f.newArgusWatcherController(nil, nil, nil)

	received := make(chan string)
	awc.syncHandler = f.syncHandlerSendArgusWatcherName(awc, aw1, received)

	stopCh := make(chan struct{})
	defer close(stopCh)

	go wait.Until(awc.runWorker, 10*time.Millisecond, stopCh)

	aw2.Spec.LogFormat = "{foo} {bar}"
	aw2.Spec.Subjects = []*argusv1alpha1.ArgusWatcherSubject{{
		Paths:  []string{"/foo"},
		Events: []string{"bar"},
	}}
	awc.updateArgusWatcher(aw1, &aw2)
	expected := sets.NewString(aw2.Name)
	for _, name := range expected.List() {
		t.Logf("Expecting update for %+v", name)
		select {
		case got := <-received:
			if !expected.Has(got) {
				t.Errorf("Expected keys %#v got %v", expected, got)
			}
		case <-time.After(wait.ForeverTestTimeout):
			t.Errorf("Expected update notifications for argus watchers")
		}
	}
}

func TestWatchPods(t *testing.T) {
	f := newFixture(t)
	fakeWatch := watch.NewFake()
	kubeclient := kubefake.NewSimpleClientset()
	kubeclient.PrependWatchReactor("pods", core.DefaultWatchReactor(fakeWatch, nil))
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	awc := f.newArgusWatcherController(kubeclient, nil, nil)

	received := make(chan string)
	// The pod update sent through the fakeWatcher should figure out the
	// managing ArgusWatcher and send it into the syncHandler.
	awc.syncHandler = f.syncHandlerCheckArgusWatcherSynced(awc, aw, received)

	stopCh := make(chan struct{})
	defer close(stopCh)

	// Start only the pod watcher and the workqueue, send a watch event, and
	// make sure it hits the sync method for the right ArgusWatcher.
	go f.kubeinformers.Core().V1().Pods().Informer().Run(stopCh)
	go awc.Run(1, stopCh)

	pods := newPodList("bar", aw, nil, 1, corev1.PodRunning, awMatchedLabel, false)
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
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	awc := f.newArgusWatcherController(kubeclient, nil, nil)

	received := make(chan string)
	// The pod update sent through the fakeWatcher should figure out the
	// managing ArgusWatcher and send it into the syncHandler.
	awc.syncHandler = f.syncHandlerCheckArgusWatcherSynced(awc, aw, received)

	stopCh := make(chan struct{})
	defer close(stopCh)

	// Start only the pod watcher and the workqueue, send a watch event, and
	// make sure it hits the sync method for the right ArgusWatcher.
	go f.kubeinformers.Core().V1().Pods().Informer().Run(stopCh)
	go awc.Run(1, stopCh)

	pod := newPod("bar", aw, corev1.PodRunning, true, true)
	fakeWatch.Add(pod)

	daemon := newDaemonPod("baz", aw)
	fakeWatch.Add(daemon)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}
}

func TestAddPodBeingDeleted(t *testing.T) {
	f := newFixture(t)
	fakeWatch := watch.NewFake()
	kubeclient := kubefake.NewSimpleClientset()
	kubeclient.PrependWatchReactor("pods", core.DefaultWatchReactor(fakeWatch, nil))
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod := newPod("bar", aw, corev1.PodRunning, true, true)
	pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	awc := f.newArgusWatcherController(kubeclient, nil, nil)

	received := make(chan string)
	// The pod update sent through the fakeWatcher should figure out the
	// managing ArgusWatcher and send it into the syncHandler.
	awc.syncHandler = f.syncHandlerCheckArgusWatcherSynced(awc, aw, received)

	stopCh := make(chan struct{})
	defer close(stopCh)

	// Start only the pod watcher and the workqueue, send a watch event, and
	// make sure it hits the sync method for the right ArgusWatcher.
	go f.kubeinformers.Core().V1().Pods().Informer().Run(stopCh)
	go awc.Run(1, stopCh)

	fakeWatch.Add(pod)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}
}

func TestUpdatePods(t *testing.T) {
	f := newFixture(t)
	aw1 := newArgusWatcher("foo", awMatchedLabel)
	aw2 := *aw1
	aw2.Spec.Selector = &metav1.LabelSelector{MatchLabels: awNonMatchedLabel}
	aw2.Name = "bar"
	f.awLister = append(f.awLister, aw1, &aw2)
	f.objects = append(f.objects, aw1, &aw2)
	awc := f.newArgusWatcherController(nil, nil, nil)

	received := make(chan string)
	awc.syncHandler = f.syncHandlerSendArgusWatcherName(awc, aw1, received)

	stopCh := make(chan struct{})
	defer close(stopCh)

	go wait.Until(awc.runWorker, 10*time.Millisecond, stopCh)

	// case 1: Pod without ArgusD watcher and new ResourceVersion should enqueue
	// update.
	pod1 := newPodList("bar", aw1, f.kubeinformers.Core().V1().Pods().Informer().GetIndexer(),
		1, corev1.PodRunning, awMatchedLabel, false).Items[0]
	pod1.ResourceVersion = "1"
	pod2 := pod1
	pod2.Labels = awNonMatchedLabel
	pod2.ResourceVersion = "2"
	awc.updatePod(&pod1, &pod2)
	expected := sets.NewString(aw2.Name)
	for _, name := range expected.List() {
		t.Logf("Expecting update for %+v", name)
		select {
		case got := <-received:
			if !expected.Has(got) {
				t.Errorf("Expected keys %#v got %v", expected, got)
			}
		case <-time.After(wait.ForeverTestTimeout):
			t.Errorf("Expected update notifications for argus watchers")
		}
	}

	// case 2: Pod without ArgusD watcher and same ResourceVersion should not
	// enqueue update.
	pod1 = *newPod("bar", aw1, corev1.PodRunning, true, false)
	pod1.ResourceVersion = "2"
	pod1.Labels = awNonMatchedLabel
	pod2 = pod1
	pod2.ResourceVersion = "2"
	awc.updatePod(&pod1, &pod2)
	t.Logf("Not expecting update for %+v", aw2.Name)
	select {
	case got := <-received:
		t.Errorf("Not expecting update for %v", got)
	case <-time.After(time.Millisecond):
	}
}

func TestDeleteDaemonPod(t *testing.T) {
	f := newFixture(t)
	fakeWatch := watch.NewFake()
	kubeclient := kubefake.NewSimpleClientset()
	kubeclient.PrependWatchReactor("pods", core.DefaultWatchReactor(fakeWatch, nil))
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	awc := f.newArgusWatcherController(kubeclient, nil, nil)

	received := make(chan string)
	// The pod update sent through the fakeWatcher should figure out the
	// managing ArgusWatcher and send it into the syncHandler.
	awc.syncHandler = f.syncHandlerSendArgusWatcherName(awc, aw, received)

	stopCh := make(chan struct{})
	defer close(stopCh)

	// Start only the pod watcher and the workqueue, send a watch event, and
	// make sure it hits the sync method for the right ArgusWatcher.
	go f.kubeinformers.Core().V1().Pods().Informer().Run(stopCh)
	go awc.Run(1, stopCh)

	pod := newPod("bar", aw, corev1.PodRunning, true, false)
	fakeWatch.Add(pod)

	daemon := newDaemonPod("baz", aw)
	ep := newEndpoint(argusdService, daemon)
	f.kubeinformers.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
	fakeWatch.Add(daemon)

	select {
	case <-received:
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("unexpected timeout from result channel")
	}

	fakeWatch.Delete(daemon)

	annotationMux.RLock()
	defer annotationMux.RUnlock()
	if _, found := pod.Annotations[ArgusWatcherAnnotationKey]; found {
		t.Errorf("Expected pod annotations to be updated %#v", pod.Name)
	}
}

func TestDeleteFinalStateUnknown(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod := newPod("bar", aw, corev1.PodRunning, false, true)
	awc := f.newArgusWatcherController(nil, nil, nil)

	received := make(chan string)
	awc.syncHandler = func(key string) error {
		received <- key
		return nil
	}
	// The DeletedFinalStateUnknown object should cause the ArgusWatcher manager
	// to insert the controller matching the selectors of the deleted pod into
	// the work queue.
	awc.deletePod(cache.DeletedFinalStateUnknown{
		Key: "foo",
		Obj: pod,
	})
	go awc.runWorker()

	expected := GetKey(aw, t)
	select {
	case key := <-received:
		if key != expected {
			t.Errorf("Unexpected sync all for ArgusWatchers %v, expected %v", key, expected)
		}
	case <-time.After(wait.ForeverTestTimeout):
		t.Errorf("Processing DeleteFinalStateUnknown took longer than expected")
	}
}

func TestControllerUpdateRequeue(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	aw.Status = argusv1alpha1.ArgusWatcherStatus{ObservablePods: 2}
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	client := fake.NewSimpleClientset(f.objects...)
	client.PrependReactor("update", "arguswatchers", func(action core.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "status" {
			return false, nil, nil
		}
		return true, nil, errors.New("failed to update status")
	})
	awc := f.newArgusWatcherController(nil, client, nil)

	// This server should force a requeue of the controller because it fails to
	// update status.ObservablePods.
	newPodList("bar", aw, f.kubeinformers.Core().V1().Pods().Informer().GetIndexer(),
		1, corev1.PodRunning, awMatchedLabel, false)

	// Enqueue once. Then process it. Disable rate-limiting for this.
	awc.workqueue = workqueue.NewRateLimitingQueue(workqueue.NewMaxOfRateLimiter())
	awc.enqueueArgusWatcher(aw)
	awc.processNextWorkItem()
	// It should have been requeued.
	if got, want := awc.workqueue.Len(), 1; got != want {
		t.Errorf("queue.Len() = %v, want %v", got, want)
	}
}

func TestControllerUpdateWithFailure(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	awc := f.newArgusWatcherController(nil, nil, nil)

	awc.workqueue.AddRateLimited(nil)
	awc.processNextWorkItem()
	// It should have errored.
	if got, want := awc.workqueue.Len(), 0; got != want {
		t.Errorf("queue.Len() = %v, want %v", got, want)
	}
}

func TestControllerUpdateStatusWithFailure(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	client := fake.NewSimpleClientset(f.objects...)
	client.PrependReactor("get", "arguswatchers", func(action core.Action) (bool, runtime.Object, error) {
		return true, aw, nil
	})
	client.PrependReactor("*", "*", func(action core.Action) (bool, runtime.Object, error) {
		return true, &argusv1alpha1.ArgusWatcher{}, fmt.Errorf("Fake error")
	})
	f.newArgusWatcherController(nil, client, nil)

	numObservablePods := int32(10)
	expectedAW := aw.DeepCopy()
	expectedAW.Status.ObservablePods = numObservablePods
	f.expectUpdateArgusWatcherAction(expectedAW)
	f.expectGetArgusWatcherAction(expectedAW)

	newStatus := argusv1alpha1.ArgusWatcherStatus{ObservablePods: numObservablePods}
	updateArgusWatcherStatus(f.client.ArguscontrollerV1alpha1().ArgusWatchers(aw.Namespace), aw, newStatus)
	f.verifyActions()
}

// TestArgusWatcherSyncExpectations tests that a pod cannot sneak in between
// counting active pods and checking expectations.
func TestArgusWatcherSyncExpectations(t *testing.T) {
	f := newFixture(t)
	awc := f.newArgusWatcherController(nil, nil, nil)

	aw := newArgusWatcher("foo", awMatchedLabel)
	f.argusinformers.Arguscontroller().V1alpha1().ArgusWatchers().Informer().GetIndexer().Add(aw)
	pods := newPodList("bar", aw, nil, 2, corev1.PodPending, awMatchedLabel, false)
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Add(&pods.Items[0])
	postExpectationsPod := pods.Items[1]

	awc.expectations = controller.NewUIDTrackingControllerExpectations(FakeAWExpectations{
		controller.NewControllerExpectations(), true, func() {
			// If we check active pods before checking expectataions, the
			// ArgusWatcher will create a new watcher because it doesn't see this
			// pod, but has fulfilled its expectations.
			f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Add(&postExpectationsPod)
		},
	})

	f.expectUpdateArgusWatcherStatusAction(aw)
	awc.syncArgusWatcher(GetKey(aw, t))
}

func TestArgusWatcherSyncToCreateWatcher(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod := newPod("bar", aw, corev1.PodRunning, true, true)
	daemon := newDaemonPod("baz", aw)
	ep := newEndpoint(argusdService, daemon)
	f.podLister = append(f.podLister, pod, daemon)
	f.endpointsLister = append(f.endpointsLister, ep)
	f.kubeobjects = append(f.kubeobjects, pod, daemon, ep)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := &pb.ArgusdHandle{NodeName: podNodeName}
	fc := newMockArgusdClient(ctrl)
	stubGetWatchState(ctrl, fc, handle)
	awc := f.newArgusWatcherController(nil, nil, fc)

	stubCreateWatch(ctrl, fc, handle)
	awc.syncArgusWatcher(GetKey(aw, t))

	awKey, err := controller.KeyFunc(aw)
	if err != nil {
		f.t.Errorf("Couldn't get key for object %#v: %v", aw, err)
	}
	f.waitForPodExpectationFulfillment(awc, awKey, pod)
}

func TestArgusWatcherSyncToDestroyWatcher(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod := newPod("bar", aw, corev1.PodRunning, true, true)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{ContainerID: "abc123"}}
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := &pb.ArgusdHandle{
		NodeName: podNodeName,
		PodName:  "bar",
	}
	fc := newMockArgusdClient(ctrl)
	fc.handle = handle
	stubGetWatchState(ctrl, fc, handle)
	awc := f.newArgusWatcherController(nil, nil, fc)

	stubDestroyWatch(ctrl, fc)
	pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	awc.syncArgusWatcher(GetKey(aw, t))
}

func TestDeleteControllerAndExpectations(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod := newPod("bar", aw, corev1.PodRunning, true, true)
	daemon := newDaemonPod("baz", aw)
	ep := newEndpoint(argusdService, daemon)
	f.podLister = append(f.podLister, pod, daemon)
	f.endpointsLister = append(f.endpointsLister, ep)
	f.kubeobjects = append(f.kubeobjects, pod, daemon, ep)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	handle := &pb.ArgusdHandle{NodeName: podNodeName}
	fc := newMockArgusdClient(ctrl)
	stubGetWatchState(ctrl, fc, handle)
	awc := f.newArgusWatcherController(nil, nil, fc)

	stubCreateWatch(ctrl, fc, handle)
	// This should set expectations for the ArgusWatcher
	awc.syncArgusWatcher(GetKey(aw, t))

	// Get the ArgusWatcher key.
	awKey, err := controller.KeyFunc(aw)
	if err != nil {
		t.Errorf("Couldn't get key for object %#v: %v", aw, err)
	}
	f.waitForPodExpectationFulfillment(awc, awKey, pod)

	// This is to simulate a concurrent addPod, that has a handle on the
	// expectations as the controller deletes it.
	podExp, exists, err := awc.expectations.GetExpectations(awKey)
	if !exists || err != nil {
		t.Errorf("No expectations found for ArgusWatcher")
	}

	f.argusinformers.Arguscontroller().V1alpha1().ArgusWatchers().Informer().GetIndexer().Delete(aw)
	awc.syncArgusWatcher(GetKey(aw, t))
	if !podExp.Fulfilled() {
		t.Errorf("Found expectations, expected none since the ArgusWatcher has been deleted.")
	}

	// This should have no effect, since we've deleted the ArgusWatcher.
	podExp.Add(-1, 0)
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Replace(make([]interface{}, 0), "0")
	awc.syncArgusWatcher(GetKey(aw, t))
}

func TestDeletionTimestamp(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod1 := newPodList("bar", aw, nil, 1, corev1.PodRunning, awMatchedLabel, true).Items[0]
	pod1.ResourceVersion = "1"
	pod1.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	awc := f.newArgusWatcherController(nil, nil, nil)

	awKey, err := controller.KeyFunc(aw)
	if err != nil {
		t.Errorf("Couldn't get key for object %#v: %v", aw, err)
	}
	awc.expectations.ExpectDeletions(awKey, []string{controller.PodKey(&pod1)})

	// A pod added with a deletion timestamp should decrement deletions, not
	// creations.
	awc.addPod(&pod1)

	queueAW, _ := awc.workqueue.Get()
	if queueAW != awKey {
		t.Fatalf("Expected to find key %v in queue, found %v", awKey, queueAW)
	}
	awc.workqueue.Done(awKey)

	podExp, exists, err := awc.expectations.GetExpectations(awKey)
	if !exists || err != nil || !podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}

	// An update from no deletion timestamp to having one should be treated as
	// a deletion.
	pod2 := newPodList("baz", aw, nil, 1, corev1.PodPending, awMatchedLabel, false).Items[0]
	pod2.ResourceVersion = "2"
	awc.expectations.ExpectDeletions(awKey, []string{controller.PodKey(&pod1)})
	awc.updatePod(&pod2, &pod1)

	queueAW, _ = awc.workqueue.Get()
	if queueAW != awKey {
		t.Fatalf("Expected to find key %v in queue, found %v", awKey, queueAW)
	}
	awc.workqueue.Done(awKey)

	podExp, exists, err = awc.expectations.GetExpectations(awKey)
	if !exists || err != nil || !podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}

	// An update to the pod (including an update to the deletion timestamp)
	// should not be counted as a second delete.
	pod3 := newPod("qux", aw, corev1.PodRunning, true, true)
	awc.expectations.ExpectDeletions(awKey, []string{controller.PodKey(pod3)})
	pod2.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	pod2.ResourceVersion = "2"
	pod2.Labels = awNonMatchedLabel
	awc.updatePod(&pod2, &pod1)

	podExp, exists, err = awc.expectations.GetExpectations(awKey)
	if !exists || err != nil || podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}

	// A pod with a non-nil deletion timestamp should also be ignored by the
	// delete handler, because it's already been counted in the update.
	awc.deletePod(&pod1)
	podExp, exists, err = awc.expectations.GetExpectations(awKey)
	if !exists || err != nil || podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}

	// Deleting the second pod should clear expectations.
	awc.deletePod(pod3)

	queueAW, _ = awc.workqueue.Get()
	if queueAW != awKey {
		t.Fatalf("Expected to find key %v in queue, found %v", awKey, queueAW)
	}
	awc.workqueue.Done(awKey)

	podExp, exists, err = awc.expectations.GetExpectations(awKey)
	if !exists || err != nil || !podExp.Fulfilled() {
		t.Fatalf("Wrong expectations %#v", podExp)
	}
}

func TestOverlappingArgusWatchers(t *testing.T) {
	f := newFixture(t)
	// Create 10 ArgusWatchers, shuffled them randomly and insert them into the
	// ArgusWatcher controller's store. All use the same CreationTimestamp.
	timestamp := metav1.Date(2000, time.January, 0, 0, 0, 0, 0, time.Local)
	var controllers []*argusv1alpha1.ArgusWatcher
	for i := 1; i < 10; i++ {
		aw := newArgusWatcher(fmt.Sprintf("aw%d", i), awMatchedLabel)
		aw.CreationTimestamp = timestamp
		controllers = append(controllers, aw)
	}
	shuffledControllers := shuffle(controllers)
	for i := range shuffledControllers {
		f.awLister = append(f.awLister, shuffledControllers[i])
		f.objects = append(f.objects, shuffledControllers[i])
	}
	// Add a pod and make sure only the corresponding ArgusWatcher is synced.
	// Pick a AW in the middle since the old code used to sort by name if all
	// timestamps were equal.
	aw := controllers[3]
	pod := newPodList("bar", aw, nil, 1, corev1.PodRunning, awMatchedLabel, true).Items[0]
	f.podLister = append(f.podLister, &pod)
	f.kubeobjects = append(f.kubeobjects, &pod)
	awc := f.newArgusWatcherController(nil, nil, nil)

	awKey := GetKey(aw, t)
	awc.addPod(&pod)

	queueAW, _ := awc.workqueue.Get()
	if queueAW != awKey {
		t.Fatalf("Expected to find key %v in queue, found %v", awKey, queueAW)
	}
}

func TestPodControllerLookup(t *testing.T) {
	f := newFixture(t)
	awc := f.newArgusWatcherController(nil, nil, nil)

	testCases := []struct {
		inAWs       []*argusv1alpha1.ArgusWatcher
		pod         *corev1.Pod
		outAWName   string
		expectError bool
	}{{
		// Pods without labels don't match any ArgusWatchers.
		inAWs: []*argusv1alpha1.ArgusWatcher{{
			ObjectMeta: metav1.ObjectMeta{Name: "lorem"},
		}},
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: metav1.NamespaceAll,
			},
		},
		outAWName: "",
	}, {
		// Matching labels, not namespace.
		inAWs: []*argusv1alpha1.ArgusWatcher{{
			ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			Spec: argusv1alpha1.ArgusWatcherSpec{
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
		outAWName: "",
	}, {
		// Matching namespace and labels returns the key to the ArgusWatcher, not
		// the ArgusWatcher name.
		inAWs: []*argusv1alpha1.ArgusWatcher{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "ipsum",
			},
			Spec: argusv1alpha1.ArgusWatcherSpec{
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
		outAWName: "bar",
	}, {
		// Pod with invalid labelSelector causes an error.
		inAWs: []*argusv1alpha1.ArgusWatcher{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "ipsum",
			},
			Spec: argusv1alpha1.ArgusWatcherSpec{
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
		outAWName:   "",
		expectError: true,
	}, {
		// More than one ArgusWatcher selected for a pod creates an error.
		inAWs: []*argusv1alpha1.ArgusWatcher{{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "ipsum",
			},
			Spec: argusv1alpha1.ArgusWatcherSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"foo": "bar"},
				},
			},
		}, {
			ObjectMeta: metav1.ObjectMeta{
				Name:      "baz",
				Namespace: "ipsum",
			},
			Spec: argusv1alpha1.ArgusWatcherSpec{
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
		outAWName:   "",
		expectError: true,
	}}

	for _, tc := range testCases {
		for _, aw := range tc.inAWs {
			f.argusinformers.Arguscontroller().V1alpha1().ArgusWatchers().Informer().GetIndexer().Add(aw)
		}
		if aws := awc.getPodArgusWatchers(tc.pod); aws != nil {
			if len(aws) > 1 && tc.expectError {
				continue
			} else if len(aws) != 1 {
				t.Errorf("len(aws) = %v, want %v", len(aws), 1)
				continue
			}
			aw := aws[0]
			if tc.outAWName != aw.Name {
				t.Errorf("Got argus watcher %+v expected %+v", aw.Name, tc.outAWName)
			}
		} else if tc.outAWName != "" {
			t.Errorf("Expected a argus watcher %v pod %v, found none", tc.outAWName, tc.pod.Name)
		}
	}
}

func TestGetPodKeys(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod1 := newPod("bar", aw, corev1.PodRunning, true, true)
	pod2 := newPod("baz", aw, corev1.PodRunning, true, true)
	f.podLister = append(f.podLister, pod1, pod2)
	f.kubeobjects = append(f.kubeobjects, pod1, pod2)
	f.newArgusWatcherController(nil, nil, nil)

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
		[]string{"argus/bar", "argus/baz"},
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

func TestUpdatePodOnceValid(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping updatePodOnceValid in short mode")
	}

	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	fc := newMockArgusdClient(ctrl)
	awc := f.newArgusWatcherController(nil, nil, fc)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bar",
			Namespace: aw.Namespace,
		},
		Spec: corev1.PodSpec{NodeName: ""},
		Status: corev1.PodStatus{
			HostIP:            "",
			ContainerStatuses: []corev1.ContainerStatus{{}},
		},
	}
	f.podLister = append(f.podLister, pod)
	// Failure: host name/ip not available
	awc.updatePodOnceValid(pod.Name, aw)
	if fc.handle != nil {
		t.Errorf("expected handle to be nil: %v", fc.handle)
	}

	pod.Spec.NodeName = podNodeName
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Update(pod)
	// Failure: pod container id not available
	awc.updatePodOnceValid(pod.Name, aw)
	if fc.handle != nil {
		t.Errorf("expected handle to be nil: %v", fc.handle)
	}

	pod.Status.HostIP = podHostIP
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Update(pod)
	// Failure: available pod container count does not match ready
	awc.updatePodOnceValid(pod.Name, aw)
	if fc.handle != nil {
		t.Errorf("expected handle to be nil: %v", fc.handle)
	}

	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{ContainerID: "abc123"}}
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Update(pod)
	// Failure: available pod container count does not match ready
	awc.updatePodOnceValid(pod.Name, aw)
	if fc.handle != nil {
		t.Errorf("expected handle to be nil: %v", fc.handle)
	}

	pod.Spec.Containers = []corev1.Container{{Name: "baz"}}
	pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Update(pod)
	// No-op: pod is being deleted
	awc.updatePodOnceValid(pod.Name, aw)

	pod.DeletionTimestamp = nil
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Update(pod)
	handle := &pb.ArgusdHandle{NodeName: podNodeName}
	stubCreateWatch(ctrl, fc, handle)
	// Successful call to CreateWatch
	awc.updatePodOnceValid(pod.Name, aw)
	if !reflect.DeepEqual(fc.handle, handle) {
		t.Errorf("Handle does not match\nDiff:\n %s", diff.ObjectGoPrintDiff(fc.handle, handle))
	}
}

func TestGetHostURL(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	pod := newPod("bar", aw, corev1.PodRunning, true, false)
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	awc := f.newArgusWatcherController(nil, nil, nil)

	if hostURL, err := awc.getHostURL(pod); err == nil {
		f.t.Errorf("expected hostURL to be in error state: %v", hostURL)
	}
	pod.Status.PodIP = podIP
	if hostURL, err := awc.getHostURL(pod); err == nil {
		t.Errorf("expected hostURL to be in error state: %v", hostURL)
	}

	daemon := newDaemonPod("baz", aw)
	ep := newEndpoint(argusdService, daemon)
	f.kubeinformers.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
	if hostURL, err := awc.getHostURL(pod); err == nil {
		t.Errorf("expected hostURL to be in error state: %v", hostURL)
	}
	ep.Subsets[0].Addresses[0].TargetRef.Name = pod.Name
	if hostURL, err := awc.getHostURL(pod); err != nil {
		t.Errorf("expected hostURL to be valid: %v", hostURL)
	}
}

func TestGetHostURLFromSiblingPod(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	pod := newPod("bar", aw, corev1.PodRunning, true, false)
	f.podLister = append(f.podLister, pod)
	f.kubeobjects = append(f.kubeobjects, pod)
	awc := f.newArgusWatcherController(nil, nil, nil)

	if hostURL, err := awc.getHostURLFromSiblingPod(pod); err == nil {
		t.Errorf("expected hostURL to be in error state: %v", hostURL)
	}
	pod.Status.PodIP = podIP
	if hostURL, err := awc.getHostURLFromSiblingPod(pod); err == nil {
		t.Errorf("expected hostURL to be in error state: %v", hostURL)
	}

	daemon := newDaemonPod("baz", aw)
	ep := newEndpoint(argusdService, daemon)
	f.kubeinformers.Core().V1().Pods().Informer().GetIndexer().Add(daemon)
	f.kubeinformers.Core().V1().Endpoints().Informer().GetIndexer().Add(ep)
	if hostURL, err := awc.getHostURLFromSiblingPod(pod); err != nil {
		t.Errorf("expected hostURL to be valid: %v", hostURL)
	}
}

func TestGetArgusWatcherSubjects(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	aw.Spec.Subjects = []*argusv1alpha1.ArgusWatcherSubject{{
		Paths:      []string{"/foo"},
		Events:     []string{"bar"},
		Ignore:     []string{"/baz"},
		OnlyDir:    true,
		Recursive:  true,
		MaxDepth:   2,
		FollowMove: true,
	}}
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	awc := f.newArgusWatcherController(nil, nil, nil)

	subjects := awc.getArgusWatcherSubjects(aw)
	if got, want := len(subjects), 1; got != want {
		t.Errorf("unexpected watcher subjects, expected %v, got %v", want, got)
	}
	expSubject := &pb.ArgusWatcherSubject{
		Path:       []string{"/foo"},
		Event:      []string{"bar"},
		Ignore:     []string{"/baz"},
		OnlyDir:    true,
		Recursive:  true,
		MaxDepth:   2,
		FollowMove: true,
	}
	if !reflect.DeepEqual(expSubject, subjects[0]) {
		t.Errorf("Subject does not match\nDiff:\n %s", diff.ObjectGoPrintDiff(expSubject, subjects[0]))
	}
}

func TestWatchStates(t *testing.T) {
	f := newFixture(t)
	aw := newArgusWatcher("foo", awMatchedLabel)
	f.awLister = append(f.awLister, aw)
	f.objects = append(f.objects, aw)
	pod := newPod("bar", aw, corev1.PodRunning, true, true)
	daemon := newDaemonPod("baz", aw)
	ep := newEndpoint(argusdService, daemon)
	f.podLister = append(f.podLister, pod, daemon)
	f.endpointsLister = append(f.endpointsLister, ep)
	f.kubeobjects = append(f.kubeobjects, pod, daemon, ep)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	fc := newMockArgusdClient(ctrl)
	stubGetWatchState(ctrl, fc, &pb.ArgusdHandle{PodName: daemon.Name})
	awc := f.newArgusWatcherController(nil, nil, fc)

	watchStates, _ := awc.getWatchStates()
	if got, want := len(watchStates), 1; got != want {
		t.Errorf("unexpected watch states, expected %v, got %v", want, got)
	}

	if awc.isPodInWatchState(pod, watchStates) == true {
		t.Errorf("did not expect pod to be in watch states: %v", pod.Name)
	}
	if awc.isPodInWatchState(daemon, watchStates) == false {
		t.Errorf("expected pod to be in watch states: %v", daemon.Name)
	}
}
