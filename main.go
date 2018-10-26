package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/golang/glog"
	//"github.com/grpc-ecosystem/go-grpc-prometheus"
	//"github.com/prometheus/client_golang/prometheus"
	//"github.com/prometheus/client_golang/prometheus/promauto"
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

var (
	masterURL      string
	kubeconfig     string
	fimdURL        string
	healthPort     uint
	prometheusPort uint

	/*
		// Create a summary to track fictional interservice RPC latencies for three
		// distinct services with different latency distributions. These services are
		// differentiated via a "service" label.
		rpcDurations = promauto.NewSummaryVec(
			prometheus.SummaryOpts{
				Name:       "fim_rpc_durations_seconds",
				Help:       "gRPC latency distributions.",
				Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
			},
			[]string{"service"},
		)
		// The same as above, but now as a histogram, and only for the normal
		// distribution. The buckets are targeted to the parameters of the
		// normal distribution, with 20 buckets centered on the mean, each
		// half-sigma wide.
		rpcDurationsHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "fim_rpc_durations_histogram_seconds",
			Help:    "gRPC latency distributions.",
			Buckets: prometheus.LinearBuckets(*normMean-5**normDomain, .5**normDomain, 20),
		})
	*/
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
	//go recordMetrics()
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", prometheusPort), nil)
}

func init() {
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&fimdURL, "fimd", "", "The address of the FimD server. Only required if daemon is running out-of-cluster.")
	flag.UintVar(&healthPort, "health", 5000, "The port to use for setting up the health check that will be used to monitor the controller.")
	flag.UintVar(&prometheusPort, "prometheus", 2112, "The port to use for setting up Prometheus metrics. This can be used by the cluster Prometheus to scrape data.")

	// Register the summary and the histogram with Prometheus's default registry.
	//prometheus.MustRegister(rpcDurations)
	//prometheus.MustRegister(rpcDurationsHistogram)
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
	go servePrometheus()

	if err := controller.Run(2, stopCh); err != nil {
		log.Fatalf("Error running controller: %s", err.Error())
	}
}
