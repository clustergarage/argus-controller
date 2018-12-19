# argus-controller

[![go report](https://goreportcard.com/badge/github.com/clustergarage/argus-controller?style=flat-square)](https://goreportcard.com/report/github.com/clustergarage/argus-controller)
[![Docker Automated build](https://img.shields.io/docker/build/clustergarage/argus-controller.svg?style=flat-square)](https://hub.docker.com/r/clustergarage/argus-controller)

This repository implements a Kubernetes controller for watching ArgusWatcher resources as defined with a CustomResourceDefinition.

**Note**: `go get` or `go mod` this package as `clustergarage.io/argus-controller`

It leverages the Kubernetes [client-go library](https://github.com/kubernetes/client-go/tree/master/tools/cache) to interact with various mechanisms explained in [these docs](https://github.com/kubernetes/sample-controller/blob/master/docs/controller-client-go.md).

## Purpose

This controller is used primarily to communicate between a running cluster and the [argusd](https://github.com/clustergarage/argusd) daemons running alongside it.

Included within the controller are some mechanisms to speak to the Kubernetes API and the daemons to:

- Gain insights into pods, endpoints, and custom ArgusWatcher CRDs being added, updated, and deleted.
- Perform current daemon state check of watchers that it is currently hosting.
- Notify the need to create and delete a filesystem watcher.
- Perform health checks for readiness and liveness probes in Kubernetes.

## Usage

#### Prerequisites

- `go v1.11+` &mdash; for runtime, including module support
- `gRPC` &mdash; as a communication protocol to the daemon
- `Protobuf` &mdash; for a common type definition

```
# enable Go module support
export GO111MODULE=on
```

### Cloning

**Note**: The GOPATH environment variable specifies the location of your workspace. If no GOPATH is set, it is assumed to be $HOME/go on Unix systems and %USERPROFILE%\go on Windows. If you want to use a custom location as your workspace, you can set the GOPATH environment variable.

```
mkdir -p $GOPATH/src/clustergarage.io && cd $_
git clone git@github.com/clustergarage/argus-controller

# optional: pre-download required go modules
go mod download
```

### Running

**Note**: This assumes you have a working kubeconfig, not required if operating in-cluster.

```
go run . -kubeconfig=$HOME/.kube/config
```

Or optionally connect to a locally-running daemon:

```
# run without secure credentials
go run . -kubeconfig=$HOME/.kube/config \
  -argusd localhost:50051

# run with secure credentials
go run . -kubeconfig=$HOME/.kube/config \
  -argusd localhost:50051 \
  -tls \
  -tls-ca-cert /etc/ssl/ca.pem \
  -tls-client-cert /etc/ssl/cert.pem \
  -tls-client-key /etc/ssl/key.pem \
  -tls-server-name localhost
```

**Warning**: When running the controller and daemon out-of-cluster in a VM-based Kubernetes context, the daemon will fail to locate the PID from the container ID through numerous cgroup checks and will be unable to start any watchers. When using Minikube, you can `minikube mount` the daemon folder, `minikube ssh` into it and run it inside the VM. Then point the controller at the IP/Port running inside the VM with the `-argusd` flag.

---

#### Usage of `argus-controller`:

```
Main set of flags for connecting to the Kuberetes client and API server; hooking directly into a locally-running ArgusD server:

  -kubeconfig string
        Path to a kubeconfig. Only required if out-of-cluster.
  -master string
        The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.
  -tls
        Connect to the ArgusD server using TLS. (default: false)
  -tls-ca-cert string
        The file containing trusted certificates for verifying the server. (with -tls, optional)
  -tls-client-cert string
        The file containing the client certificate for authenticating with the server. (with -tls, optional)
  -tls-client-key string
        The file containing the client private key for authenticating with the server. (with -tls)
  -tls-server-name string
        Override the hostname used to verify the server certificate. (with -tls)
  -tls-skip-verify
        Do not verify the certificate presented by the server. (default: false)
  -argusd string
        The address of the ArgusD server. Only required if daemon is running out-of-cluster.
  -health integer
        The port to use for setting up the health check that will be used to monitor the controller.
  -prometheus integer
        The port to use for setting up Prometheus metrics. This can be used by the cluster Prometheus to scrape data.

Flags for the glog logging library:

  -alsologtostderr
        log to standard error as well as files
  -log_backtrace_at value
        when logging hits line file:N, emit a stack trace
  -log_dir string
        If non-empty, write log files in this directory
  -logtostderr
        log to standard error instead of files
  -stderrthreshold value
        logs at or above this threshold go to stderr (default INFO)
  -v value
        log level for V logs
  -vmodule value
        comma-separated list of pattern=N settings for file-filtered logging
```

### Building

To build a local copy of the binary to run or troubleshoot with:

```
go build -o bin/argus-controller
```

Or if you wish to build as a Docker container and run this from a local registry:

```
docker build -t clustergarage/argus-controller .
```

## Testing

### Unit Tests

Unit tests included for the controller behavior can be run with:

```
go test pkg/controller/*
```

Running the tests skipping long-running tests can be done with the `-short` flag:

```
go test -short pkg/controller/*
```

Optionally, running with [code coverage](https://blog.golang.org/cover) and generating an HTML report of the results:

```
go test -cover -coverprofile coverage/cover.out pkg/controller/*
go tool cover -html coverage/cover.out
```

### Integration Tests

Integration tests included for the controller behavior can be run with:

```
go test -tags=integration pkg/controller/*
```

---

#### Code Verification

```
go vet pkg/controller/*
golint pkg/controller/*
```

## Custom Resource Definition

Each instance of the ArgusWatcher custom resource has an attached `Spec`, which is defined via a `struct{}` to provide data format validation. In practice, this `Spec` is arbitrary key-value data that specifies the configuration/behavior of the resource.

```go
type ArgusWatcherSpec struct {
  Selector  *metav1.LabelSelector `json:"selector"`
  Subjects  []*ArgusWatcherSubject  `json:"subjects"`
  LogFormat string                `json:"logFormat"`
}

type ArgusWatcherSubject struct {
  Paths      []string          `json:"paths"`
  Events     []string          `json:"events"`
  Ignore     []string          `json:"ignore"`
  OnlyDir    bool              `json:"onlyDir"`
  Recursive  bool              `json:"recursive"`
  MaxDepth   int32             `json:"maxDepth"`
  FollowMove bool              `json:"followMove"`
  Tags       map[string]string `json:"tags,omitempty"`
}
```

### Generating Definitions

Making use of generators to generate a typed client, informers, listers, and deep-copy functions, you can run this yourself with:

**Note**: `code-generator` needs to be in the `vendor` folder until upstream Kubernetes updates to Go v1.11 for modules support.

```
# deepcopy-gen has to be installed in `$GOPATH/bin`
go get -u k8s.io/code-generator/cmd/deepcopy-gen

./bin/update-codegen.sh
```

The `update-codegen` script will automatically generate the following files and directories:

- `pkg/apis/arguscontroller/v1alpha1/zz_generated.deepcopy.go`
- `pkg/client/`

Changes should not be made manually to these files. When updating the definitions of `pkg/apis/arguscontroller/*` you should re-run the `update-codegen` script to regenerate the files listed above.

## Cleanup

You can clean up the created CustomResourceDefinition with:

```
kubectl delete crd arguswatchers.arguscontroller.clustergarage.io
```

## Documentation

To view `godoc`-style documentation generated from the code, run the following then navigate to `http://localhost:6060/pkg/clustergarage.io`:

```
godoc -http ":6060"
```
