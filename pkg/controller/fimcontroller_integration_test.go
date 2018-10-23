// +build integration

package fimcontroller

/*
import (
	"net/http/httptest"
	"testing"
	"time"

	fimv1alpha1 "clustergarage.io/fim-controller/pkg/apis/fimcontroller/v1alpha1"
	fimclientset "clustergarage.io/fim-controller/pkg/client/clientset/versioned"
	informers "clustergarage.io/fim-controller/pkg/client/informers/externalversions"
	kubeinformers "k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/kubernetes/test/integration/framework"
)

func fwcSetup(t *testing.T) (*httptest.Server, framework.CloseFunc, *fimv1alpha1.FimWatcherController,
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
	fiminformers := informers.NewSharedInformerFactory(fimclientset.NewForConfigOrDie(restclient.AddUserAgent(&config, "fim-informers")), resyncPeriod)

	fwc := NewFimWatcherController(
		clientset.NewForConfigOrDie(restclient.AddUserAgent(&config, "kube-controller")),
		fimclientset.NewForConfigOrDie(restclient.AddUserAgent(&config, "fim-controller")),
		fiminformers.Fimcontroller().V1alpha1().FimWatchers(),
		kubeinformers.Core().V1().Pods(),
		kubeinformers.Core().V1().Endpoints(),
		nil)

	if err != nil {
		t.Fatalf("Failed to create fimwatcher controller")
	}
	return s, closeFn, fwc, kubeinformers, fiminformers, clientSet
}
*/
