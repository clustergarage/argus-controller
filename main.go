package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	// StatusInvalidArguments indicates specified invalid arguments.
	StatusInvalidArguments = 1
)

var (
	masterURL      string
	kubeconfig     string
	fimdURL        string
	tls            bool
	tlsSkipVerify  bool
	tlsCACert      string
	tlsClientCert  string
	tlsClientKey   string
	tlsServerName  string
	healthPort     uint
	prometheusPort uint
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
	address, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", healthPort))
	if err != nil {
		log.Fatalf("Error dialing health check server port: %v", err)
	}
	if err := server.Serve(address); err != nil {
		log.Fatalf("Error serving health check server: %v", err)
	}
}

func servePrometheus() {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", prometheusPort), nil)
}

func init() {
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&fimdURL, "fimd", "", "The address of the FimD server. Only required if daemon is running out-of-cluster.")
	flag.BoolVar(&tls, "tls", false, "Connect to the FimD server using TLS. (default: false)")
	flag.BoolVar(&tlsSkipVerify, "tls-skip-verify", false, "Do not verify the certificate presented by the server. (default: false)")
	flag.StringVar(&tlsCACert, "tls-ca-cert", "", "The file containing trusted certificates for verifying the server. (with -tls, optional)")
	flag.StringVar(&tlsClientCert, "tls-client-cert", "", "The file containing the client certificate for authenticating with the server. (with -tls, optional)")
	flag.StringVar(&tlsClientKey, "tls-client-key", "", "The file containing the client private key for authenticating with the server. (with -tls)")
	flag.StringVar(&tlsServerName, "tls-server-name", "", "Override the hostname used to verify the server certificate. (with -tls)")
	flag.UintVar(&healthPort, "health", 5000, "The port to use for setting up the health check that will be used to monitor the controller.")
	flag.UintVar(&prometheusPort, "prometheus", 2112, "The port to use for setting up Prometheus metrics. This can be used by the cluster Prometheus to scrape data.")

	argError := func(s string, v ...interface{}) {
		log.Printf("error: "+s, v...)
		os.Exit(StatusInvalidArguments)
	}

	if !tls {
		if tlsSkipVerify {
			argError("Specified -tls-skip-verify without specifying -tls.")
		}
		if tlsCACert != "" {
			argError("Specified -tls-ca-cert without specifying -tls.")
		}
		if tlsClientCert != "" {
			argError("Specified -tls-client-cert without specifying -tls.")
		}
		if tlsServerName != "" {
			argError("Specified -tls-server-name without specifying -tls.")
		}
	}
	if tlsClientCert != "" && tlsClientKey == "" {
		argError("Specified -tls-client-cert without specifying -tls-client-key.")
	}
	if tlsClientCert == "" && tlsClientKey != "" {
		argError("Specified -tls-client-key without specifying -tls-client-cert.")
	}
	if tlsSkipVerify {
		if tlsCACert != "" {
			argError("Cannot specify -tls-ca-cert with -tls-skip-verify (CA cert would not be used).")
		}
		if tlsServerName != "" {
			argError("Cannot specify -tls-server-name with -tls-skip-verify (server name would not be used).")
		}
	}
}

func main() {
	// Override --stderrthreshold default value.
	if stderrThreshold := flag.Lookup("stderrthreshold"); stderrThreshold != nil {
		stderrThreshold.DefValue = "INFO"
		stderrThreshold.Value.Set("INFO")
	}
	flag.Parse()
	defer glog.Flush()

	// Set up signals so we handle the first shutdown signal gracefully.
	stopCh := signals.SetupSignalHandler()

	kubeclientset, fimclientset := getKubernetesClient()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclientset, time.Second*30)
	fimInformerFactory := informers.NewSharedInformerFactory(fimclientset, time.Second*30)

	opts, err := fimcontroller.BuildAndStoreDialOptions(tls, tlsSkipVerify, tlsCACert, tlsClientCert, tlsClientKey, tlsServerName)
	if err != nil {
		log.Fatalf("Error creating dial options: %s", err.Error())
	}
	fimdConnection, err := fimcontroller.NewFimdConnection(fimdURL, opts)

	if err != nil {
		log.Fatalf("Error creating connection to FimD server: %s", err.Error())
	}

	controller := fimcontroller.NewFimWatcherController(kubeclientset, fimclientset,
		fimInformerFactory.Fimcontroller().V1alpha1().FimWatchers(),
		kubeInformerFactory.Core().V1().Pods(),
		kubeInformerFactory.Core().V1().Endpoints(),
		fimdConnection)

	go kubeInformerFactory.Start(stopCh)
	go fimInformerFactory.Start(stopCh)
	go serveHealthCheck()
	go servePrometheus()

	if err := controller.Run(2, stopCh); err != nil {
		log.Fatalf("Error running controller: %s", err.Error())
	}
}
