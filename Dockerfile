FROM golang:1.11.1 as builder
ENV GO111MODULE on
ENV GRPC_HEALTH_PROBE_VERSION v0.2.0
WORKDIR /go/src/clustergarage.io/fim-controller/
# Put dependencies in their own layer so they are cached.
COPY go.mod go.sum ./
RUN go mod download
# Build the rest of the code.
COPY . /go/src/clustergarage.io/fim-controller/
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o fimcontroller .
# Include grpc_health_probe for K8s liveness/readiness checks.
RUN wget -qO/bin/grpc_health_probe \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
  chmod +x /bin/grpc_health_probe

FROM alpine:edge
COPY --from=builder /go/src/clustergarage.io/fim-controller/fimcontroller /
COPY --from=builder /bin/grpc_health_probe /bin/
## glog requires /tmp to exist as log_dir is /tmp by default.
COPY --from=builder /tmp /tmp
CMD ["/fimcontroller"]
