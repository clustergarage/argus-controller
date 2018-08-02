FROM golang:latest as builder
WORKDIR /go/src/clustergarage.io/fim-controller/
COPY . /go/src/clustergarage.io/fim-controller/
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o fimcontroller .

FROM scratch
COPY --from=builder /go/src/clustergarage.io/fim-controller/fimcontroller /
# glog seems to require /tmp to exist for writing logs by default
COPY --from=builder /tmp /tmp
CMD ["/fimcontroller"]
