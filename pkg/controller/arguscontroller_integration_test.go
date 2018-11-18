// +build integration

package arguscontroller

/*
import (
	"net/http/httptest"
	"testing"
	"time"

	argusv1alpha1 "clustergarage.io/argus-controller/pkg/apis/arguscontroller/v1alpha1"
	argusclientset "clustergarage.io/argus-controller/pkg/client/clientset/versioned"
	informers "clustergarage.io/argus-controller/pkg/client/informers/externalversions"
	kubeinformers "k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/kubernetes/test/integration/framework"
)

func awcSetup(t *testing.T) (*httptest.Server, framework.CloseFunc, *argusv1alpha1.ArgusWatcherController,
	kubeinformers.SharedInformerFactory, informers.SharedInformerFactory, clientset.Interface) {

	masterConfig := framework.NewIntegrationTestMasterConfig()
	_, s, closeFn := framework.RunAMaster(masterConfig)

	config := restclient.Config{Host: s.URL}
	clientSet, err := clientset.NewForConfig(&config)
	if err != nil {
		t.Fatalf("Error in create clientset: %v", err)
	}
	resyncPeriod := 12 * time.Hour
	kubeinformers = kubeinformers.NewSharedInformerFactory(clientset.NewForConfigOrDie(restclient.AddUserAgent(&config, "kube-informers")), resyncPeriod)
	argusinformers := informers.NewSharedInformerFactory(argusclientset.NewForConfigOrDie(restclient.AddUserAgent(&config, "argus-informers")), resyncPeriod)

	awc := NewArgusWatcherController(
		clientset.NewForConfigOrDie(restclient.AddUserAgent(&config, "kube-controller")),
		argusclientset.NewForConfigOrDie(restclient.AddUserAgent(&config, "argus-controller")),
		argusinformers.arguscontroller().V1alpha1().ArgusWatchers(),
		kubeinformers.Core().V1().Pods(),
		kubeinformers.Core().V1().Endpoints(),
		nil)

	if err != nil {
		t.Fatalf("Failed to create arguswatcher controller")
	}
	return s, closeFn, awc, kubeinformers, argusinformers, clientSet
}
*/
