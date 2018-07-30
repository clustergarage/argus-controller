# fim-controller

## Cloning Repository

```
cd $GOPATH/src/clustergarage.io
git clone git@github.com/clustergarage/fim-controller
```

## Building

```
# save Godeps
godep save

# build kube-controller definitions
# NOTE: one may need to `git clone https://github.com/kubernetes/code-generator.git`
#       into vendor/k8s.io if pkg is not up-to-date
./bin/update-codegen.sh

# build binary
go bin -o bin/fim-controller .
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
