# fim-controller

[![go report](https://goreportcard.com/badge/github.com/clustergarage/fim-controller?style=flat-square)](https://goreportcard.com/report/github.com/clustergarage/fim-controller)

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

# build binary
go build -o bin/fim-controller .
```

## Running Locally

```
# connect to locally-running FimD daemon
./bin/fim-controller -kubeconfig $HOME/.kube/config -fimd 0.0.0.0:50051
```

## Running Tests

```
go test pkg/controller/*

# run with code coverage
go test -cover -coverprofile coverage/out pkg/controller/*
# parse code coverage into html
go tool cover -html coverage/out
```

## Verifying Code

```
go vet pkg/controller/*
golint pkg/controller/*
```

## Updating Controller Types

```
# NOTE: code-generator needs to be in the `vendor` folder until
#       Kubernetes updates to Go v1.11 for modules support

# deepcopy-gen has to be installed in $GOPATH/bin
go get -u k8s.io/code-generator/cmd/deepcopy-gen

# build kube-controller definitions
./bin/update-codegen.sh
```

## Documentation

```
# run local godoc server
godoc -http ":6060"

# navigate to http://localhost:6060/pkg/clustergarage.io
```
