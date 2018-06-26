# fim-k8s

## Cloning repo

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

# build binary
go bin -o bin/fimctl .
```
