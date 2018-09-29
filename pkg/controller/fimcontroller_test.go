package fimcontroller

import (
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/diff"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	fimv1alpha1 "clustergarage.io/fim-controller/pkg/apis/fimcontroller/v1alpha1"
	"clustergarage.io/fim-controller/pkg/client/clientset/versioned/fake"
	informers "clustergarage.io/fim-controller/pkg/client/informers/externalversions"
	pb "github.com/clustergarage/fim-proto/golang"
	pbmock "github.com/clustergarage/fim-proto/golang_mock"
)

const (
	fwGroup    = "fimcontroller.clustergarage.io"
	fwVersion  = "v1alpha1"
	fwResource = "fimwatchers"
	fwKind     = "FimWatcher"
	fwName     = "test"
	fwHostURL  = "fimd:50051"
)

var (
	alwaysReady        = func() bool { return true }
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *k8sfake.Clientset
	// Objects to put in the store.
	fwLister  []*fimv1alpha1.FimWatcher
	podLister []*corev1.Pod
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
	f.objects = []runtime.Object{}
	f.kubeobjects = []runtime.Object{}
	return f
}

func newFimWatcher(name string) *fimv1alpha1.FimWatcher {
	return &fimv1alpha1.FimWatcher{
		TypeMeta: metav1.TypeMeta{APIVersion: fimv1alpha1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: fimv1alpha1.FimWatcherSpec{
			//Selector: ,
			//Subjects: ,
			//LogFormat: ,
		},
	}
}

func (f *fixture) newController(fimdConnection *FimdConnection) (*FimWatcherController, kubeinformers.SharedInformerFactory, informers.SharedInformerFactory) {
	f.client = fake.NewSimpleClientset(f.objects...)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeobjects...)

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(f.kubeclient, noResyncPeriodFunc())
	fimInformerFactory := informers.NewSharedInformerFactory(f.client, noResyncPeriodFunc())

	controller := NewFimWatcherController(f.kubeclient, f.client,
		fimInformerFactory.Fimcontroller().V1alpha1().FimWatchers(),
		kubeInformerFactory.Core().V1().Pods(),
		fimdConnection)

	controller.fwListerSynced = alwaysReady
	controller.podListerSynced = alwaysReady
	controller.recorder = &record.FakeRecorder{}

	for _, p := range f.podLister {
		kubeInformerFactory.Core().V1().Pods().Informer().GetIndexer().Add(p)
	}
	for _, fw := range f.fwLister {
		fimInformerFactory.Fimcontroller().V1alpha1().FimWatchers().Informer().GetIndexer().Add(fw)
	}

	return controller, kubeInformerFactory, fimInformerFactory
}

func newMockServer(t *testing.T) (*gomock.Controller, *pbmock.MockFimdClient, *pbmock.MockFimd_GetWatchStateClient) {
	ctrl := gomock.NewController(t)
	client := pbmock.NewMockFimdClient(ctrl)
	stream := pbmock.NewMockFimd_GetWatchStateClient(ctrl)
	return ctrl, client, stream
}

func (f *fixture) run(fwName string, fimdConnection *FimdConnection) {
	f.runController(fwName, fimdConnection, true, false)
}

func (f *fixture) runExpectError(fwName string, fimdConnection *FimdConnection) {
	f.runController(fwName, fimdConnection, true, true)
}

func (f *fixture) runController(fwName string, fimdConnection *FimdConnection, startInformers bool, expectError bool) {
	controller, kubeInformerFactory, fimInformerFactory := f.newController(fimdConnection)
	if startInformers {
		stopCh := make(chan struct{})
		defer close(stopCh)
		kubeInformerFactory.Start(stopCh)
		fimInformerFactory.Start(stopCh)
	}

	err := controller.syncHandler(fwName)
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

	k8sActions := filterInformerActions(f.kubeclient.Actions())
	for i, action := range k8sActions {
		if len(f.kubeactions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(k8sActions)-len(f.kubeactions), k8sActions[i:])
			break
		}
		expectedAction := f.kubeactions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.kubeactions) > len(k8sActions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.kubeactions)-len(k8sActions), f.kubeactions[len(k8sActions):])
	}
}

// checkAction verifies that expected and actual actions are equal and both have
// same attached resources
func checkAction(expected, actual core.Action, t *testing.T) {
	if !(expected.Matches(actual.GetVerb(), actual.GetResource().Resource) && actual.GetSubresource() == expected.GetSubresource()) {
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
			(action.Matches("list", "fws") ||
				action.Matches("watch", "fws") ||
				action.Matches("list", "pods") ||
				action.Matches("watch", "pods")) {
			continue
		}
		ret = append(ret, action)
	}

	return ret
}

func (f *fixture) expectUpdateFWStatusAction(fw *fimv1alpha1.FimWatcher) {
	action := core.NewUpdateAction(schema.GroupVersionResource{
		Group:    fwGroup,
		Version:  fwVersion,
		Resource: fwResource,
	}, fw.Namespace, fw)
	// TODO: Until #38113 is merged, we can't use Subresource
	//action.Subresource = "status"
	f.actions = append(f.actions, action)
}

func getKey(fw *fimv1alpha1.FimWatcher, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(fw)
	if err != nil {
		t.Errorf("Unexpected error getting key for fw %v: %v", fw.Name, err)
		return ""
	}
	return key
}

func TestDoNothing(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher(fwName)

	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)

	f.expectUpdateFWStatusAction(fw)
	f.run(getKey(fw, t), nil)
}

func TestUsingFimdURL(t *testing.T) {
	f := newFixture(t)
	fw := newFimWatcher(fwName)

	ctrl, client, stream := newMockServer(t)
	defer ctrl.Finish()

	// Expect successful Recv message.
	stream.EXPECT().Recv().Return(&pb.FimdHandle{}, nil)
	// Expect io.EOF message to break out of loop.
	stream.EXPECT().Recv().Return(nil, io.EOF)
	client.EXPECT().GetWatchState(gomock.Any(), gomock.Any()).Return(stream, nil)

	f.fwLister = append(f.fwLister, fw)
	f.objects = append(f.objects, fw)

	f.expectUpdateFWStatusAction(fw)
	f.run(getKey(fw, t), &FimdConnection{
		hostURL: fwHostURL,
		client:  client,
	})
}
