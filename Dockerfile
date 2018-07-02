FROM golang:latest as builder
WORKDIR /go/src/clustergarage.io/fim-k8s/
COPY . /go/src/clustergarage.io/fim-k8s/
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o fimcontroller .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /go/src/clustergarage.io/fim-k8s/fimcontroller .
CMD ["./fimcontroller"]
