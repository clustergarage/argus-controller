package main

import (
	"flag"
	"log"
	"time"

	"github.com/golang/glog"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	clientset "clustergarage.io/fim-controller/pkg/client/clientset/versioned"
	informers "clustergarage.io/fim-controller/pkg/client/informers/externalversions"
	fimcontroller "clustergarage.io/fim-controller/pkg/controller"
	"clustergarage.io/fim-controller/pkg/signals"
)

var (
	masterURL  string
	kubeconfig string
	fimdURL    string
)

func getKubernetesClient() (kubernetes.Interface, clientset.Interface) {
	config, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %s", err.Error())
	}
	kubeclientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}
	fimclientset, err := clientset.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error building fim clientset: %s", err.Error())
	}
	return kubeclientset, fimclientset
}

func main() {
	// Override --alsologtostderr default value.
	if alsoLogToStderr := flag.Lookup("alsologtostderr"); alsoLogToStderr != nil {
		alsoLogToStderr.DefValue = "true"
		alsoLogToStderr.Value.Set("true")
	}
	flag.Parse()
	defer glog.Flush()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	kubeclientset, fimclientset := getKubernetesClient()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclientset, time.Second*30)
	fimInformerFactory := informers.NewSharedInformerFactory(fimclientset, time.Second*30)

	controller := fimcontroller.NewFimWatcherController(kubeclientset, fimclientset,
		fimInformerFactory.Fimcontroller().V1alpha1().FimWatchers(),
		kubeInformerFactory.Core().V1().Pods(),
		kubeInformerFactory.Core().V1().Services(),
		fimdURL)

	go kubeInformerFactory.Start(stopCh)
	go fimInformerFactory.Start(stopCh)

	if err := controller.Run(2, stopCh); err != nil {
		log.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&fimdURL, "fimd", "", "The address of the FimD server. Only required if daemon is running out-of-cluster.")
}
