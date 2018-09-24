# fim-controller

## Prerequisites

- go v1.11+
- gRPC
- Protobuf

## Cloning Repository

```
cd $GOPATH/src/clustergarage.io
git clone git@github.com/clustergarage/fim-controller
```

## Building

```
# enable Go module support
export GO111MODULE=on

# download required go modules
go mod download

# NOTE: code-generator needs to be in the `vendor` folder until
#       Kubernetes updates to Go v1.11 for modules support

# build kube-controller definitions
./bin/update-codegen.sh

# build binary
go bin -o bin/fim-controller .

# prepare for a module release (git tag)
go mod tidy
```

## Running Locally

```
# connect to locally-running FimD daemon
./bin/fim-controller -kubeconfig $HOME/.kube/config -fimd 0.0.0.0:50051
```

## Verifying Code

```
go vet pkg/controller/fimcontroller.go pkg/controller/fimdcontroller_utils.go
golint pkg/controller/fimcontroller.go pkg/controller/fimdcontroller_utils.go
```
