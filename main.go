package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/golang/glog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	clientset "clustergarage.io/fim-controller/pkg/client/clientset/versioned"
	informers "clustergarage.io/fim-controller/pkg/client/informers/externalversions"
	fimcontroller "clustergarage.io/fim-controller/pkg/controller"
	"clustergarage.io/fim-controller/pkg/signals"
)

const (
	HealthCheckPort = 5000
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

func serveHealthCheck() {
	server := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(server, health.NewServer())
	address, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", HealthCheckPort))
	if err != nil {
		log.Fatalf("Error dialing health check server port: %v", err)
	}
	if err := server.Serve(address); err != nil {
		log.Fatalf("Error serving health check server: %v", err)
	}
}

func init() {
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&fimdURL, "fimd", "", "The address of the FimD server. Only required if daemon is running out-of-cluster.")
}

func main() {
	// Override --stderrthreshold default value.
	if stderrThreshold := flag.Lookup("stderrthreshold"); stderrThreshold != nil {
		stderrThreshold.DefValue = "INFO"
		stderrThreshold.Value.Set("INFO")
	}
	flag.Parse()
	defer glog.Flush()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	kubeclientset, fimclientset := getKubernetesClient()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclientset, time.Second*30)
	fimInformerFactory := informers.NewSharedInformerFactory(fimclientset, time.Second*30)

	var fimdConnection *fimcontroller.FimdConnection
	if fimdURL != "" {
		fimdConnection = fimcontroller.NewFimdConnection(fimdURL)
	}

	controller := fimcontroller.NewFimWatcherController(kubeclientset, fimclientset,
		fimInformerFactory.Fimcontroller().V1alpha1().FimWatchers(),
		kubeInformerFactory.Core().V1().Pods(),
		kubeInformerFactory.Core().V1().Endpoints(),
		fimdConnection)

	go kubeInformerFactory.Start(stopCh)
	go fimInformerFactory.Start(stopCh)
	go serveHealthCheck()

	if err := controller.Run(2, stopCh); err != nil {
		log.Fatalf("Error running controller: %s", err.Error())
	}
}
