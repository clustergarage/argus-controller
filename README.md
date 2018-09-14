# fim-controller

## Cloning Repository

```
cd $GOPATH/src/clustergarage.io
git clone git@github.com/clustergarage/fim-controller
```

## Building

```
# enable Go module support
export GO111MODULE=on

# NOTE: code-generator needs to be in a vendor/ folder still until
# Kubernetes updates to Go v1.11 for modules support
#
# build kube-controller definitions
./bin/update-codegen.sh

# build binary
go bin -o bin/fim-controller .

# prepare for a module release (git tag)
go mod tidy
```

## Running Locally

```
./bin/fim-controller -kubeconfig $HOME/.kube/config -log_dir ./log
```
