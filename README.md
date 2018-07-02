# fim-k8s

## Cloning Repository

```
cd $GOPATH/src/clustergarage.io
git clone git@github.com/clustergarage/fim-k8s
```

## Building

```
# save Godeps
godep save ./...

# build protobuf definitions
protoc -I fimlet/ fimlet/fimlet.proto --go_out=plugins=grpc:fimlet

# build kube-controller definitions
./bin/update-codegen.sh

# build binary
go bin -o bin/fim-controller .
```

## Preparing CustomResourceDefinitions

```
kubectl apply -f configs/fim-k8s-crd.yaml
```

## Defining a FimWatcher component

```
apiVersion: fimcontroller.clustergarage.io/v1alpha1
kind: FimWatcher
metadata: [...]
spec:
  selector:
    matchLabels:
      run: myapp
  subjects:
  - paths:
    - /var/log/myapp
    events:
    - open
    - modify
  - paths:
    - /var/log/financialdata
    events:
    - all
```


## Running Locally

```
./bin/fim-controller -kubeconfig $HOME/.kube/config -log_dir ./log
```
