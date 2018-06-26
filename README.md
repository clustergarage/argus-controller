# fim-k8s

```
# save Godeps
godep save ./...

# build protobuf definitions
protoc -I fimlet/ fimlet/fimlet.proto --go_out=plugins=grpc:fimlet
```
